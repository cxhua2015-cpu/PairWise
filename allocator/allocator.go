package allocator
import "errors"
var(ErrInvalidOptions=errors.New("invalid options");ErrInvalidInput=errors.New("invalid input");ErrNotFound=errors.New("not found");ErrConflict=errors.New("conflict");ErrNoSpace=errors.New("no space");ErrCapacity=errors.New("capacity exceeded"))
type Options struct{Size uint64;MaxAllocations,MaxNameBytes int};type OpKind uint8
const(Reserve OpKind=iota+1;Allocate;Free)
type Op struct{Kind OpKind;Name string;Start,Length,Alignment uint64};type Batch struct{Ops []Op};type Allocation struct{Name string;Start,Length,Revision uint64};type Result struct{Generation,Revision uint64;Changed []Allocation};type Snapshot struct{Generation,NextRevision,Size uint64;Allocations []Allocation};type Allocator struct{}
func New(Options)(*Allocator,error){return nil,ErrInvalidOptions};func(*Allocator)Apply(Batch)(Result,error){return Result{},ErrInvalidInput};func(*Allocator)Find(uint64,uint64)(uint64,bool,error){return 0,false,ErrInvalidInput};func(*Allocator)Lookup(string)(Allocation,bool,error){return Allocation{},false,ErrInvalidInput};func(*Allocator)Snapshot()Snapshot{return Snapshot{}}
