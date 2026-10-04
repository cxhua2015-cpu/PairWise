package bimap
import "errors"
var(ErrInvalidOptions=errors.New("invalid options");ErrInvalidInput=errors.New("invalid input");ErrNotFound=errors.New("not found");ErrMismatch=errors.New("pair mismatch");ErrConflict=errors.New("conflict");ErrCapacity=errors.New("capacity exceeded"))
type Options struct{MaxPairs,MaxNameBytes int};type OpKind uint8
const(Bind OpKind=iota+1;Unbind)
type Op struct{Kind OpKind;Left,Right string};type Batch struct{Ops []Op};type Pair struct{Left,Right string;Revision uint64};type Result struct{Generation,Revision uint64;Changed []Pair};type Snapshot struct{Generation,NextRevision uint64;Pairs []Pair};type Registry struct{}
func New(Options)(*Registry,error){return nil,ErrInvalidOptions};func(*Registry)Apply(Batch)(Result,error){return Result{},ErrInvalidInput};func(*Registry)LookupLeft(string)(Pair,bool,error){return Pair{},false,ErrInvalidInput};func(*Registry)LookupRight(string)(Pair,bool,error){return Pair{},false,ErrInvalidInput};func(*Registry)List(string,int)([]Pair,error){return nil,ErrInvalidInput};func(*Registry)Snapshot()Snapshot{return Snapshot{}}
