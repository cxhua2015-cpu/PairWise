package main

import (
	"fmt"
	"log"

	"example.com/pairwise/pathrouter/router"
)

func main() {
	r, err := router.New(router.Options{MaxRoutes: 8})
	if err != nil {
		log.Fatal(err)
	}
	_, err = r.ApplyBatch([]router.Change{
		{Type: router.ChangeAdd, Route: router.Route{Method: "GET", Pattern: "/users/:id", Name: "user", Value: []byte("profile")}},
		{Type: router.ChangeAdd, Route: router.Route{Method: "POST", Pattern: "/users", Name: "create", Value: []byte("create")}},
	})
	if err != nil {
		log.Fatal(err)
	}
	m, err := r.Match("HEAD", "/users/alice")
	if err != nil {
		log.Fatal(err)
	}
	path, err := r.Build("user", map[string]string{"id": "alice"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("found=%t name=%s fallback=%t id=%s path=%s generation=%d\n", m.Found, m.RouteName, m.HeadFallback, m.Params[0].Value, path, m.Generation)
}
