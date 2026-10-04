package main
import("fmt";"example.com/pairwise/allocator/allocator")
func main(){a,_:=allocator.New(allocator.Options{Size:64,MaxAllocations:8,MaxNameBytes:16});x,_:=a.Apply(allocator.Batch{Ops:[]allocator.Op{{Kind:allocator.Reserve,Name:"fixed",Start:0,Length:8},{Kind:allocator.Allocate,Name:"auto",Length:4,Alignment:8}}});s:=a.Snapshot();fmt.Printf("generation=%d revision=%d allocations=%d autoStart=%d\n",x.Generation,x.Revision,len(s.Allocations),s.Allocations[1].Start)}
