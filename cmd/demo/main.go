package main
import("fmt";"example.com/pairwise/ledger/ledger")
func main(){l,_:=ledger.New(ledger.Options{MaxAccounts:8,MaxNameBytes:16});x,_:=l.Apply(ledger.Batch{Ops:[]ledger.Op{{Kind:ledger.Open,Account:"alice",Amount:10},{Kind:ledger.Open,Account:"bob"},{Kind:ledger.Transfer,Account:"alice",Other:"bob",Amount:3}}});a,_,_:=l.Get("alice");b,_,_:=l.Get("bob");fmt.Printf("generation=%d revision=%d alice=%d bob=%d\n",x.Generation,x.Revision,a.Balance,b.Balance)}
