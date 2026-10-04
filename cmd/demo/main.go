package main
import("fmt";"example.com/pairwise/bimap/bimap")
func main(){r,_:=bimap.New(bimap.Options{MaxPairs:8,MaxNameBytes:16});x,_:=r.Apply(bimap.Batch{Ops:[]bimap.Op{{Kind:bimap.Bind,Left:"user-a",Right:"node-1"},{Kind:bimap.Bind,Left:"user-b",Right:"node-2"}}});p,_,_:=r.LookupLeft("user-a");fmt.Printf("generation=%d revision=%d pairs=%d right=%s\n",x.Generation,x.Revision,len(r.Snapshot().Pairs),p.Right)}
