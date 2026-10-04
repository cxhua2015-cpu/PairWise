package main
import("fmt";"example.com/pairwise/breaker/breaker")
func main(){r,_:=breaker.New(breaker.Options{MaxServices:2,MaxNameBytes:16},[]breaker.Policy{{Name:"api",FailureThreshold:2,RecoveryThreshold:1,OpenFor:5}});x,_:=r.Record(breaker.Batch{Now:1,Events:[]breaker.Event{{Service:"api"},{Service:"api"}}});ok,s,_:=r.Allow("api",1);fmt.Printf("generation=%d changed=%d allowed=%t mode=%d openUntil=%d\n",x.Generation,len(x.Changed),ok,s.Mode,s.OpenUntil)}
