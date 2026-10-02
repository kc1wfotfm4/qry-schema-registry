package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kc1wfotfm4/qry-schema-registry/internal/schema"
	"github.com/kc1wfotfm4/qry-schema-registry/internal/store"
)

const compatibilityMessage = "compatibility must be one of NONE, BACKWARD, FORWARD, FULL"

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

// registerVersion handles POST /api/v1/subjects/:id/versions.
func registerVersion(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject := c.Param("id")

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "request body must be a valid JSON object")
			return
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(body, &doc); err != nil || doc == nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "request body must be a valid JSON object")
			return
		}

		schemaRaw, ok := doc["schema"]
		if !ok {
			writeError(c, http.StatusBadRequest, "missing_field", `missing required field "schema"`)
			return
		}
		compatibilityRaw, ok := doc["compatibility"]
		if !ok {
			writeError(c, http.StatusBadRequest, "missing_field", `missing required field "compatibility"`)
			return
		}

		var schemaText string
		if err := json.Unmarshal(schemaRaw, &schemaText); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_schema", `"schema" must be a string`)
			return
		}
		parsed, err := schema.Parse(schemaText)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_schema", err.Error())
			return
		}

		var compatibility string
		if err := json.Unmarshal(compatibilityRaw, &compatibility); err != nil || !schema.ValidLevel(compatibility) {
			writeError(c, http.StatusBadRequest, "invalid_compatibility", compatibilityMessage)
			return
		}

		version, err := st.RegisterVersion(subject, schemaText, compatibility, func(prev *store.Version) error {
			previous, perr := schema.Parse(prev.Schema)
			if perr != nil {
				return nil // stored versions were validated when registered
			}
			return schema.Check(compatibility, previous, parsed)
		})
		if err != nil {
			var violation *schema.Violation
			if errors.As(err, &violation) {
				writeError(c, http.StatusConflict, "incompatible_schema", violation.Reason)
				return
			}
			writeError(c, http.StatusInternalServerError, "internal_error", "the version could not be registered")
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"subject":       version.Subject,
			"schema":        version.Schema,
			"version":       version.Version,
			"compatibility": version.Compatibility,
		})
	}
}
