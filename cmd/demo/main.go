package main
import("fmt";"example.com/pairwise/windowcounter/windowcounter")
func main(){r,_:=windowcounter.New(windowcounter.Options{Window:10,MaxKeys:8,MaxEvents:32,MaxNameBytes:16});_,_=r.Apply(windowcounter.Batch{Now:1,Deltas:[]windowcounter.Delta{{Key:"api",Amount:3}}});x,_:=r.Apply(windowcounter.Batch{Now:5,Deltas:[]windowcounter.Delta{{Key:"api",Amount:-1},{Key:"jobs",Amount:2}}});s:=r.Snapshot();fmt.Printf("generation=%d touched=%d keys=%d events=%d api=%d\n",x.Generation,len(x.Counts),len(s.Keys),s.Events,s.Keys[0].Value)}
