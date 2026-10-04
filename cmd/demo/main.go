package main
import("fmt";"example.com/pairwise/leasepool/leasepool")
func main(){r,_:=leasepool.New(leasepool.Options{MaxPools:2,MaxLeases:8,MaxNameBytes:16,MaxOwnerBytes:16},[]leasepool.PoolConfig{{Name:"cpu",Capacity:8}});x,_:=r.Apply(leasepool.Batch{Now:1,Ops:[]leasepool.Op{{Kind:leasepool.Acquire,Pool:"cpu",LeaseID:"job-1",Owner:"worker",Weight:3,ExpiresAt:5}}});s:=r.Snapshot();fmt.Printf("generation=%d pools=%d leases=%d used=%d\n",x.Generation,len(s.Pools),len(s.Leases),s.Pools[0].Used)}
