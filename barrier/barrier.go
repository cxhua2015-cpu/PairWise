package barrier
import "errors"
var(ErrInvalidOptions=errors.New("invalid options");ErrInvalidInput=errors.New("invalid input");ErrNotFound=errors.New("not found");ErrConflict=errors.New("conflict");ErrCapacity=errors.New("capacity exceeded"))
type Options struct{MaxBarriers,MaxPending,MaxNameBytes int};type Config struct{Name string;Parties int};type OpKind uint8
const(Arrive OpKind=iota+1;Cancel)
type Op struct{Kind OpKind;Barrier,Participant string};type Batch struct{Ops []Op};type Completion struct{Barrier string;Generation uint64;Participants []string};type Result struct{Generation uint64;Completions []Completion};type BarrierState struct{Name string;Parties int;Generation uint64;Pending []string};type Snapshot struct{Generation uint64;Pending int;Barriers []BarrierState};type Registry struct{}
func New(Options,[]Config)(*Registry,error){return nil,ErrInvalidOptions};func(*Registry)Apply(Batch)(Result,error){return Result{},ErrInvalidInput};func(*Registry)Snapshot()Snapshot{return Snapshot{}}
