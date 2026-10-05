# resourcecatalog136

并发安全的内存型资源目录，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构（三层联动）

- `servicecatalog.go` — 状态引擎：事务化批次、快照、容量约束。
- `policy.go` — 策略层：独立同步、可原子替换的 actor 白名单与单批操作数上限。
- `coordinator.go` — 协调层：先授权再调用引擎，为成功、拒绝、引擎失败分配连续审计序号。

## 索引

状态引擎以 `map[string]entry` 为主索引，键为资源名，值为不可变的
`{value, revision}`。Value 字节在 Put 时拷贝，此后绝不原地修改，因此候选
事务只需浅拷贝 map 头。另维护 `totalValue` 累计值避免每次重算总字节数。

## 候选事务

`Apply` 分两阶段：先对全部操作做纯结构校验（kind、名称字符集与长度、
Value 长度），不读取任何状态；随后在互斥锁内把记录 map 浅拷贝为候选
副本，按输入顺序在副本上执行 Put/Delete（Put 分配连续 revision，Delete
不分配），批次末才检查记录数与 Value 总字节容量。任一步失败直接丢弃
候选副本，状态、generation、revision 天然回滚，无需补偿日志。非空成功
批次 generation 只增一次，空批次不变。

## 所有权

- Put 的 Value 入库存储前深拷贝，调用方之后修改入参不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本。
- `Coordinator.Decisions` 返回内部切片的拷贝，调用方修改不影响审计日志。
- `Policy.ReplaceActors` 先完整构建新白名单再在写锁下一次替换，读取方
  只看到替换前或替换后的完整配置。

## 复杂度

- `Apply`：O(n + m)，n 为操作数，m 为当前记录数（候选浅拷贝）；排序
  `Changed` 为 O(k log k)，k 为去重后的名称数。
- `Get`：O(1) 平均；`Snapshot`：O(m log m)（按名称排序）。
- `Authorize`：O(1)；`Decisions`：O(d)，d 为审计条数。

## 并发

三层各自持有独立锁：引擎用 `sync.Mutex` 串行化批次，策略用
`sync.RWMutex` 支持并发授权，协调器用 `sync.Mutex` 保证审计序号连续。
所有公开方法可并发调用；`go test -race ./...` 通过。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
