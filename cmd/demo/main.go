package main
import("fmt";"example.com/pairwise/scoreboard/scoreboard")
func main(){b,_:=scoreboard.New(scoreboard.Options{MaxMembers:8,MaxNameBytes:16});x,_:=b.Apply(scoreboard.Batch{Ops:[]scoreboard.Op{{Kind:scoreboard.Upsert,Member:"alice",Score:10},{Kind:scoreboard.Upsert,Member:"bob",Score:7},{Kind:scoreboard.Increment,Member:"bob",Delta:5}}});top,_:=b.Range(0,100,scoreboard.Cursor{},2);fmt.Printf("generation=%d revision=%d leader=%s score=%d entries=%d\n",x.Generation,x.Revision,top[0].Member,top[0].Score,len(top))}
