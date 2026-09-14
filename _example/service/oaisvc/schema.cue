package oaisvc

import "strings"

// The definitions below are the single source of truth for the oai
// demo service: `cue exp gengotypes` generates the Go request and
// response types from them (see cue_types_oaisvc_gen.go), mizucue
// validates decoded requests against them, and mizucue's OpenAPI
// generation turns them into the document's component schemas.
//
// Field layout mirrors mizuoai's serde conventions: the input type
// groups fields under header/path/query/body members, and each
// field's CUE name is its wire name.

// ScrapeRequest is the input of GET /oai/scrape: one mandatory
// header.
#ScrapeRequest: {
	header: {
		// key is the magic word echoed back.
		key: string & strings.MinRunes(1) @go(Key)
	}
}

// ScrapeResponse echoes the magic word back.
#ScrapeResponse: {
	// message is the greeting.
	message: string
}

// CreateOrderRequest is the input of POST /oai/user/{user_id}/order:
// one group per request location.
#CreateOrderRequest: {
	path: {
		// user_id identifies the ordering user.
		user_id: string & strings.MinRunes(1) @go(UserId)
	}
	query: {
		// timestamp is the client's unix-second order time.
		timestamp?: int @go(UnixTime)
	}
	header: {
		// X-Region tells where the order is from.
		"X-Region"?: string @go(Region)
	}
	body: {
		// id is the client-supplied order id.
		id: string & strings.MinRunes(1) @go(Id)
		// amount is the order amount and must be positive.
		amount: int & >0
		// comment is optional free text.
		comment?: string
	}
}

// CreateOrderResponse echoes the processed amount.
#CreateOrderResponse: {
	// amount is the amount actually processed.
	amount: int
}

// DownloadPackageOperation is the OpenAPI contract of GET
// /oai/package — a raw gzip stream that reflection cannot describe.
// The members are @go(-) so the generated Go type stays empty; the
// definition name still keys the fragment for Schema.Operation,
// which mizuoai merges over the reflected operation with
// WithOperationPatch.
#DownloadPackageOperation: {
	operationId: "downloadPackage" @go(-)
	responses: "200": {
		description: "Compressed example package"
		headers: "Content-Disposition": {
			description: "Download filename"
			required:    true
			schema: type: "string"
		}
		content: "application/gzip": {}
	} @go(-)
}
