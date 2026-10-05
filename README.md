# resourcecatalog136

并发安全的内存型资源目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete；Put 分配连续 revision，Delete 不分配。完整结构校验先于状态读取，
记录数与 Value 总字节容量只在批次末检查；失败回滚全部状态、generation 与
revision。Get/Snapshot 深拷贝 Value，Snapshot 按名称排序。

## 架构（三层联动）

- `servicecatalog.go`（状态引擎）：持有事务性数据与快照。
- `policy.go`（策略层）：独立同步、可原子替换的 actor 白名单与单批操作数上限。
- `coordinator.go`（协调层）：先授权再调用状态引擎，为成功、拒绝与引擎失败
  分配连续审计序号。

## 索引

状态引擎以 `map[string]entry` 作为主索引，键为记录名，值为深拷贝后的
`value` 与 `revision`；另维护 `totalBytes` 计数避免每次重算总字节。
Snapshot 与 Apply 的 `Changed` 在返回前按名称排序，索引本身无序。

## 候选事务

Apply 先在持锁状态下把当前 map 浅拷贝为候选 map（entry 值不可变，故浅拷贝
即可隔离），所有 Put/Delete 与 revision 递增都作用于候选；容量检查在批次末
对候选执行。任一失败直接丢弃候选并返回错误，原 map、generation、nextRev
保持不动，回滚为零成本；成功则整体换入候选并仅将 generation 加一。

## 所有权

- 入参 `Op.Value` 在写入前复制，调用方之后修改不影响内部状态。
- `Get`/`Snapshot`/`Result.Changed` 中的 Value 均为新分配的副本。
- `Coordinator.Decisions()` 返回新切片，不别名内部审计日志。
- `Policy.ReplaceActors` 将白名单拷贝进新 map 后原子换入。

## 并发与复杂度

- Store 使用单一 `sync.Mutex`；Policy 使用 `sync.RWMutex`；Coordinator 用
  独立 `mutex` 保护审计序号与日志，授权与引擎调用不持有审计锁。
- 结构校验 O(L)（L 为批次输入总长度），不读状态；Apply 为
  O(R + L + K log K)，R 为当前记录数（候选拷贝），K 为变更名去重数量；
  Get O(1) 均摊 + O(|value|) 拷贝；Snapshot O(R log R)。
- 策略拒绝发生在接触核心状态之前，不读取也不修改引擎数据。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
