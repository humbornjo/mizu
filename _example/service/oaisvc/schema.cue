package oaisvc

#DownloadPackageRequest: {
	"__method":      "get" @go(-)
	"__path":        "/oai/package" @go(-)
	"__operationId": "downloadPackage" @go(-)
	"__responses": "200": {
		description: "Compressed example package"
		headers: "Content-Disposition": {
			description: "Download filename"
			required:    true
			schema: type: "string"
		}
		content: "application/gzip": {}
	} @go(-)
}

#DownloadPackageResponse: {}
