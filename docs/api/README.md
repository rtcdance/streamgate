# API Reference

| Document | Status | Description |
|----------|--------|-------------|
| [openapi.yaml](openapi.yaml) | Canonical | OpenAPI 3.0 spec — source of truth for all HTTP endpoints |
| Swagger UI | `/docs` | Renders the spec; registered only when `app.debug=true` |
| gRPC | `proto/v1/` | Proto definitions; no generated reference document |

The machine-readable spec is authoritative. It is served at `/docs/openapi.yaml` behind JWT (SG-14).
