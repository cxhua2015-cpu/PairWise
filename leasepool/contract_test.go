package leasepool

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func opts() Options { return Options{MaxPools: 4, MaxLeases: 32, MaxNameBytes: 16, MaxOwnerBytes: 16} }
func registry(t *testing.T) *Registry { t.Helper(); r,e:=New(opts(),[]PoolConfig{{Name:"cpu",Capacity:10},{Name:"gpu",Capacity:4}});if e!=nil{t.Fatal(e)};return r }
func acq(pool,id,owner string,w uint64,exp int64) Op{return Op{Kind:Acquire,Pool:pool,LeaseID:id,Owner:owner,Weight:w,ExpiresAt:exp}}

func TestNewAndValidationBeforeState(t *testing.T){
	if _,e:=New(Options{},nil);!errors.Is(e,ErrInvalidOptions){t.Fatal(e)}
	if _,e:=New(opts(),[]PoolConfig{{Name:"x",Capacity:1},{Name:"x",Capacity:2}});!errors.Is(e,ErrInvalidOptions){t.Fatal(e)}
	r:=registry(t);_,_=r.Apply(Batch{Now:1,Ops:[]Op{acq("cpu","l1","alice",2,3)}});before:=r.Snapshot()
	_,e:=r.Apply(Batch{Now:4,Ops:[]Op{{Kind:Release,LeaseID:"l1"},{Kind:Renew,LeaseID:"bad?",ExpiresAt:8}}})
	if !errors.Is(e,ErrInvalidInput)||!reflect.DeepEqual(before,r.Snapshot()){t.Fatalf("e=%v s=%+v",e,r.Snapshot())}
}
func TestSequentialReleaseAcquireAndRenew(t *testing.T){
	r:=registry(t);_,_=r.Apply(Batch{Now:1,Ops:[]Op{acq("cpu","old","a",10,9)}})
	x,e:=r.Apply(Batch{Now:2,Ops:[]Op{{Kind:Release,LeaseID:"old"},acq("cpu","new","b",10,6),{Kind:Renew,LeaseID:"new",ExpiresAt:8}}})
	if e!=nil||x.Generation!=2{t.Fatalf("r=%+v e=%v",x,e)};s:=r.Snapshot();if len(s.Leases)!=1||s.Leases[0].LeaseID!="new"||s.Leases[0].ExpiresAt!=8||s.Pools[0].Used!=10{t.Fatalf("s=%+v",s)}
}
func TestExpirationAndRollback(t *testing.T){
	r:=registry(t);_,_=r.Apply(Batch{Now:1,Ops:[]Op{acq("cpu","z","a",2,3),acq("gpu","a","b",1,3)}});before:=r.Snapshot()
	_,e:=r.Apply(Batch{Now:3,Ops:[]Op{{Kind:Release,LeaseID:"missing"}}});if !errors.Is(e,ErrNotFound)||!reflect.DeepEqual(before,r.Snapshot()){t.Fatalf("e=%v",e)}
	exp,e:=r.Sweep(3);if e!=nil||!reflect.DeepEqual(exp,[]string{"a","z"}){t.Fatalf("expired=%v e=%v",exp,e)}
	if r.Snapshot().Generation!=2{t.Fatalf("s=%+v",r.Snapshot())}
}
func TestConflictCapacityAndOverflow(t *testing.T){
	r:=registry(t);_,_=r.Apply(Batch{Now:1,Ops:[]Op{acq("cpu","x","a",2,9)}});before:=r.Snapshot()
	_,e:=r.Apply(Batch{Now:2,Ops:[]Op{acq("gpu","x","b",1,9)}});if !errors.Is(e,ErrConflict)||!reflect.DeepEqual(before,r.Snapshot()){t.Fatal(e)}
	_,e=r.Apply(Batch{Now:2,Ops:[]Op{acq("cpu","y","b",9,9)}});if !errors.Is(e,ErrCapacity){t.Fatal(e)}
	bigOpts:=Options{MaxPools:1,MaxLeases:4,MaxNameBytes:8,MaxOwnerBytes:8};big,_:=New(bigOpts,[]PoolConfig{{Name:"p",Capacity:math.MaxUint64}});_,_=big.Apply(Batch{Now:1,Ops:[]Op{acq("p","a","o",math.MaxUint64,9)}});_,e=big.Apply(Batch{Now:2,Ops:[]Op{acq("p","b","o",1,9)}});if !errors.Is(e,ErrCapacity){t.Fatalf("e=%v",e)}
}
func TestSnapshotOrderingAndTime(t *testing.T){
	r:=registry(t);_,_=r.Apply(Batch{Now:2,Ops:[]Op{acq("gpu","z","b",1,9),acq("cpu","a","a",2,8)}});s:=r.Snapshot();if s.Pools[0].Name!="cpu"||s.Leases[0].LeaseID!="a"{t.Fatalf("s=%+v",s)}
	before:=r.Snapshot();_,e:=r.Sweep(1);if !errors.Is(e,ErrTime)||!reflect.DeepEqual(before,r.Snapshot()){t.Fatal(e)}
}
func TestConcurrentCalls(t *testing.T){
	r,_:=New(Options{MaxPools:1,MaxLeases:128,MaxNameBytes:16,MaxOwnerBytes:8},[]PoolConfig{{Name:"p",Capacity:128}});var wg sync.WaitGroup
	for i:=0;i<64;i++{i:=i;wg.Add(1);go func(){defer wg.Done();id:=fmt.Sprintf("l-%02d",i);_,_=r.Apply(Batch{Now:1,Ops:[]Op{acq("p",id,"o",1,9)}});_=r.Snapshot()}()};wg.Wait();s:=r.Snapshot();if len(s.Leases)!=64||s.Pools[0].Used!=64{t.Fatalf("s=%+v",s)}
}
