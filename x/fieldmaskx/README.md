# fieldmaskx - JSON Field Masks for Go

Type-safe field masks bound to a Go struct, resolved with `encoding/json` visibility rules. Use a mask to whitelist response fields, prune sensitive ones, or apply partial updates.

## Features

- **Type-Safe** - Paths are validated against the struct at construction
- **JSON-Native** - Works on JSON names, honors tags, embedding, and `json:"-"`
- **Three Operations** - Keep selected fields, clear selected fields, or patch from another value

## Installation

```bash
go get github.com/humbornjo/mizu/x/fieldmaskx
```

## Quick Start

```go
package main

import (
    "fmt"

    "github.com/humbornjo/mizu/x/fieldmaskx"
)

type Profile struct {
    Name    string `json:"name"`
    Email   string `json:"email"`
    Address *struct {
        City string `json:"city"`
        Zip  string `json:"zip"`
    } `json:"address"`
}

func main() {
    // Intersect what the server allows with what the client asked for.
    mask := fieldmaskx.Intersect[Profile](
        []string{"name", "address.city"},
        []string{"name", "address", "ssn"},
    )
    fmt.Println(mask.Paths()) // [address.city name]

    profile := Profile{Name: "ada", Email: "ada@example.com"}
    _ = mask.Filter(&profile) // keeps name, clears email
}
```

Malformed, unknown, and disallowed paths are silently omitted from the mask.

## Path Syntax

Paths are dot-separated JSON field names.

- **Slices and arrays** consume no path segment: `items.label` selects `Label` on every element of `Items`.
- **String-keyed maps** consume one segment per key: `attributes.home.city`.
- **Custom marshalers are leaves**: a type implementing `json.Marshaler` or `encoding.TextMarshaler` (or whose pointer does) can be selected whole but never descended into.

Visibility follows `encoding/json`: embedded struct fields are promoted, and conflicting names resolve by depth, then explicit tag, then annihilation.

## Operations

| Operation   | Selected fields                     | Other fields | Empty mask      |
| ----------- | ----------------------------------- | ------------ | --------------- |
| `Filter`    | kept                                | cleared      | clears all      |
| `Prune`     | cleared                             | untouched    | no-op           |
| `Overwrite` | copied from source to destination   | untouched    | no-op           |

`Filter` whitelists a response, `Prune` blacks out sensitive fields, and `Overwrite` applies a partial update. Under `Overwrite`, whole pointer, map, and slice fields use normal Go assignment semantics and may alias the source.

## Examples

### Whitelisting an API Response

```go
var publicUser = fieldmaskx.Intersect[User](
    []string{"id", "name", "address.city"},
    requestedFields(r), // e.g. from a ?fields= query parameter
)

func getUser(w http.ResponseWriter, r *http.Request) {
    user := loadUser(r.PathValue("id"))
    if err := publicUser.Filter(&user); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    writeJSON(w, user)
}
```

### Applying a Partial Update

```go
var patchable = fieldmaskx.Intersect[User](
    []string{"name", "address"},
    requestedFields(r),
)

func patchUser(w http.ResponseWriter, r *http.Request) {
    patch := decodeBody[User](r)
    user := loadUser(r.PathValue("id"))
    if err := patchable.Overwrite(&patch, &user); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    saveUser(user)
}
```

Only the paths present in the request body should reach `requestedFields`; fields the client did not send stay untouched in the stored record.
