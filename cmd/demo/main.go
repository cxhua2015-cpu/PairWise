package main
import("fmt";"example.com/pairwise/prefixclaim/prefixclaim")
func main(){r,_:=prefixclaim.New(prefixclaim.Options{MaxClaims:8,MaxPathBytes:32,MaxOwnerBytes:16});x,_:=r.Apply(prefixclaim.Batch{Ops:[]prefixclaim.Op{{Kind:prefixclaim.Claim,Path:"/teams/a",Owner:"alice"},{Kind:prefixclaim.Claim,Path:"/teams/b",Owner:"bob"}}});e,ok,_:=r.Lookup("/teams/a/service");fmt.Printf("generation=%d revision=%d entries=%d found=%t owner=%s\n",x.Generation,x.Revision,len(r.Snapshot().Entries),ok,e.Owner)}
