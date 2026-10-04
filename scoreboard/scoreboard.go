package scoreboard
import "errors"
var(ErrInvalidOptions=errors.New("invalid options");ErrInvalidInput=errors.New("invalid input");ErrNotFound=errors.New("not found");ErrOverflow=errors.New("score overflow");ErrCapacity=errors.New("capacity exceeded"))
type Options struct{MaxMembers,MaxNameBytes int};type OpKind uint8
const(Upsert OpKind=iota+1;Increment;Delete)
type Op struct{Kind OpKind;Member string;Score,Delta int64};type Batch struct{Ops []Op};type Entry struct{Member string;Score int64;Revision uint64};type Result struct{Generation,Revision uint64;Changed []Entry};type Cursor struct{Set bool;Score int64;Member string};type Snapshot struct{Generation,NextRevision uint64;Entries []Entry};type Board struct{}
func New(Options)(*Board,error){return nil,ErrInvalidOptions};func(*Board)Apply(Batch)(Result,error){return Result{},ErrInvalidInput};func(*Board)Get(string)(Entry,bool,error){return Entry{},false,ErrInvalidInput};func(*Board)Range(int64,int64,Cursor,int)([]Entry,error){return nil,ErrInvalidInput};func(*Board)Snapshot()Snapshot{return Snapshot{}}
