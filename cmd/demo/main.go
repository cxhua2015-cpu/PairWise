package main

import (
	"encoding/json"
	"fmt"
	"log"

	"example.com/pairwise/jsonpatch/jsondoc"
)

func main() {
	s, err := jsondoc.New([]byte(`{"users":[{"name":"Ada"}],"enabled":true}`), jsondoc.Options{MaxNodes: 32})
	if err != nil {
		log.Fatal(err)
	}
	r, err := s.Apply(1, []jsondoc.Operation{
		{Op: "add", Path: "/users/-", Value: json.RawMessage(`{"name":"Lin"}`)},
		{Op: "replace", Path: "/enabled", Value: json.RawMessage(`false`)},
		{Op: "test", Path: "/users/0/name", Value: json.RawMessage(`"Ada"`)},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("revision=%d nodes=%d document=%s\n", r.Revision, r.Nodes, r.Document)
}
