package main
import("fmt";"example.com/pairwise/barrier/barrier")
func main(){r,_:=barrier.New(barrier.Options{MaxBarriers:2,MaxPending:8,MaxNameBytes:16},[]barrier.Config{{Name:"deploy",Parties:2}});x,_:=r.Apply(barrier.Batch{Ops:[]barrier.Op{{Kind:barrier.Arrive,Barrier:"deploy",Participant:"a"},{Kind:barrier.Arrive,Barrier:"deploy",Participant:"b"}}});s:=r.Snapshot();fmt.Printf("registryGeneration=%d completions=%d barrierGeneration=%d pending=%d\n",x.Generation,len(x.Completions),s.Barriers[0].Generation,s.Pending)}
