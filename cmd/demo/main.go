package main

import (
	"example.com/pairwise/netpolicy/netpolicy"
	"fmt"
	"log"
)

func main() {
	t, err := netpolicy.New(netpolicy.Options{MaxRules: 8, MaxValueBytes: 1024})
	if err != nil {
		log.Fatal(err)
	}
	_, err = t.ApplyBatch([]netpolicy.Change{
		{Type: netpolicy.ChangeAdd, Rule: netpolicy.Rule{ID: "default", Prefix: "10.0.0.0/8", Protocol: netpolicy.ProtocolAny, Action: netpolicy.ActionDeny}},
		{Type: netpolicy.ChangeAdd, Rule: netpolicy.Rule{ID: "web", Prefix: "10.1.2.0/24", Protocol: netpolicy.ProtocolTCP, PortStart: 443, PortEnd: 443, Priority: 10, Action: netpolicy.ActionAllow, Value: []byte("tls")}},
	})
	if err != nil {
		log.Fatal(err)
	}
	d, err := t.Match("10.1.2.9", netpolicy.ProtocolTCP, 443)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("found=%t action=%d rule=%s prefix=%s generation=%d value=%s\n", d.Found, d.Action, d.RuleID, d.Prefix, d.Generation, d.Value)
}
