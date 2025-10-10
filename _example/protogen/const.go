package protogen

 import (
   _ "embed"
 )

 var (
   //go:embed openapi.yaml
   OPENAPI []byte
 )
