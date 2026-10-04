package ledger
import "errors"
var(ErrInvalidOptions=errors.New("invalid options");ErrInvalidInput=errors.New("invalid input");ErrNotFound=errors.New("not found");ErrConflict=errors.New("conflict");ErrFunds=errors.New("insufficient funds");ErrBalance=errors.New("nonzero balance");ErrOverflow=errors.New("overflow");ErrCapacity=errors.New("capacity exceeded"))
type Options struct{MaxAccounts,MaxNameBytes int};type OpKind uint8
const(Open OpKind=iota+1;Credit;Debit;Transfer;Close)
type Op struct{Kind OpKind;Account,Other string;Amount int64};type Batch struct{Ops []Op};type Account struct{Name string;Balance int64;Revision uint64};type Result struct{Generation,Revision uint64;Changed []Account};type Snapshot struct{Generation,NextRevision uint64;Accounts []Account};type Ledger struct{}
func New(Options)(*Ledger,error){return nil,ErrInvalidOptions};func(*Ledger)Apply(Batch)(Result,error){return Result{},ErrInvalidInput};func(*Ledger)Get(string)(Account,bool,error){return Account{},false,ErrInvalidInput};func(*Ledger)Snapshot()Snapshot{return Snapshot{}}
