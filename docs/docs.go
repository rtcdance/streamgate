package docs

// SwaggerUIHTML returns the HTML for the Swagger UI page.
// This avoids importing swaggo/files which adds significant binary size.
const SwaggerUIHTML = `<!DOCTYPE html>
<html>
<head>
    <title>StreamGate API Documentation</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
SwaggerUIBundle({
    url: "/docs/swagger.yaml",
    dom_id: '#swagger-ui',
    presets: [
        SwaggerUIBundle.presets.apis,
        SwaggerUIBundle.SwaggerUIStandalonePreset
    ],
    layout: "BaseLayout"
})
</script>
</body>
</html>
`
