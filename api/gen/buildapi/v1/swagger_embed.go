package buildapiv1

import "embed"

//go:embed *.swagger.json
var SwaggerJSON embed.FS
