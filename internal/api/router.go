package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kc1wfotfm4/qry-schema-registry/internal/schema"
	"github.com/kc1wfotfm4/qry-schema-registry/internal/store"
)

// NewRouter wires the public HTTP surface. The service contract in README.md
// describes the error shape every entry must keep.
func NewRouter(st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		if err := st.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "storage_unavailable", "message": "database is not available"}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": "ok"})
	})

	router.POST("/api/v1/subjects/:id/versions", func(c *gin.Context) {
		subject, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "subject id must be an integer")
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "request body could not be read")
			return
		}
		var request map[string]json.RawMessage
		if err := json.Unmarshal(body, &request); err != nil || request == nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "request body must be a JSON object")
			return
		}

		schemaRaw, hasSchema := request["schema"]
		compatibilityRaw, hasCompatibility := request["compatibility"]
		if !hasSchema || !hasCompatibility {
			writeError(c, http.StatusBadRequest, "missing_field", "schema and compatibility are required")
			return
		}

		var schemaText string
		if err := json.Unmarshal(schemaRaw, &schemaText); err != nil {
			writeError(c, http.StatusBadRequest, "invalid_schema", "schema must be a string holding a JSON object with fields and required")
			return
		}
		definition, err := schema.Parse(schemaText)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_schema", "schema must be a JSON object with fields and required, where fields maps names to type strings and required lists declared field names")
			return
		}

		var compatibility string
		if err := json.Unmarshal(compatibilityRaw, &compatibility); err != nil || !schema.ValidLevel(compatibility) {
			writeError(c, http.StatusBadRequest, "invalid_compatibility", "compatibility must be one of NONE, BACKWARD, FORWARD, FULL")
			return
		}

		version, err := st.RegisterVersion(subject, schemaText, compatibility, func(previous string) error {
			if compatibility == schema.LevelNone {
				return nil
			}
			prev, err := schema.Parse(previous)
			if err != nil {
				return err
			}
			if !schema.Compatible(compatibility, prev, definition) {
				return schema.ErrIncompatible
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, schema.ErrIncompatible) {
				writeError(c, http.StatusConflict, "incompatible_schema", "schema violates the requested compatibility level against the previous version")
				return
			}
			writeError(c, http.StatusInternalServerError, "internal_error", "the version could not be registered")
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"subject":       subject,
			"schema":        schemaText,
			"version":       version,
			"compatibility": compatibility,
		})
	})

	router.GET("/api/v1/subjects", func(c *gin.Context) {
		subjects, err := st.ListSubjects()
		if err != nil {
			writeError(c, http.StatusInternalServerError, "internal_error", "the subjects could not be listed")
			return
		}
		c.JSON(http.StatusOK, gin.H{"subjects": subjects})
	})

	router.GET("/api/v1/subjects/:id/versions/:v", func(c *gin.Context) {
		subject, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "subject id must be an integer")
			return
		}
		version, err := strconv.ParseInt(c.Param("v"), 10, 64)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "version must be an integer")
			return
		}

		record, err := st.GetVersion(subject, version)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrSubjectNotFound):
				writeError(c, http.StatusNotFound, "subject_not_found", "subject has no registered versions")
			case errors.Is(err, store.ErrVersionNotFound):
				writeError(c, http.StatusNotFound, "version_not_found", "subject has no such version")
			default:
				writeError(c, http.StatusInternalServerError, "internal_error", "the version could not be read")
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"subject":       record.Subject,
			"schema":        record.Schema,
			"version":       record.Version,
			"compatibility": record.Compatibility,
		})
	})

	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "route_not_found", "message": "no route matches this path"}})
	})
	return router
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
