package prefixclaim
import "errors"
var(ErrInvalidOptions=errors.New("invalid options");ErrInvalidInput=errors.New("invalid input");ErrNotFound=errors.New("not found");ErrOwner=errors.New("owner mismatch");ErrConflict=errors.New("conflict");ErrCapacity=errors.New("capacity exceeded"))
type Options struct{MaxClaims,MaxPathBytes,MaxOwnerBytes int};type OpKind uint8
const(Claim OpKind=iota+1;Release)
type Op struct{Kind OpKind;Path,Owner string};type Batch struct{Ops []Op};type Entry struct{Path,Owner string;Revision uint64};type Result struct{Generation,Revision uint64;Changed []Entry};type Snapshot struct{Generation,NextRevision uint64;Entries []Entry};type Registry struct{}
func New(Options)(*Registry,error){return nil,ErrInvalidOptions};func(*Registry)Apply(Batch)(Result,error){return Result{},ErrInvalidInput};func(*Registry)Lookup(string)(Entry,bool,error){return Entry{},false,ErrInvalidInput};func(*Registry)Descendants(string,string,int)([]Entry,error){return nil,ErrInvalidInput};func(*Registry)Snapshot()Snapshot{return Snapshot{}}
