package breaker
import "errors"
var(ErrInvalidOptions=errors.New("invalid options");ErrInvalidInput=errors.New("invalid input");ErrTime=errors.New("time moved backwards");ErrNotFound=errors.New("not found");ErrOpen=errors.New("breaker open"))
type Mode uint8
const(Closed Mode=iota+1;Open;HalfOpen)
type Options struct{MaxServices,MaxNameBytes int};type Policy struct{Name string;FailureThreshold,RecoveryThreshold uint32;OpenFor int64};type Event struct{Service string;Success bool};type Batch struct{Now int64;Events []Event};type Result struct{Generation uint64;Changed []string};type ServiceState struct{Name string;Mode Mode;Failures,RecoverySuccesses uint32;OpenUntil int64};type Snapshot struct{Generation uint64;Now int64;Services []ServiceState};type Registry struct{}
func New(Options,[]Policy)(*Registry,error){return nil,ErrInvalidOptions};func(*Registry)Record(Batch)(Result,error){return Result{},ErrInvalidInput};func(*Registry)Allow(string,int64)(bool,ServiceState,error){return false,ServiceState{},ErrInvalidInput};func(*Registry)Sweep(int64)([]string,error){return nil,ErrInvalidInput};func(*Registry)Snapshot()Snapshot{return Snapshot{}}
