# balanceledger437

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision，失败整体回滚。语义详见 `SPEC.md`。

## 架构（多文件联动）

- `creditpool.go` — 核心事务引擎：`Ledger` 以 `sync.RWMutex` 保护
  `map[string]account` 主索引（值内联 revision），外加 `generation` 与
  `nextRevision` 逻辑时钟。`Apply` 先做完整结构校验，再在写锁内于
  scratch 副本上模拟语义（溢出/绝对值上限/NotFound/最终容量），全部通过
  后才提交，因此失败天然零副作用（回滚 = 不提交）。
- `validation.go` — `ValidateBatch`：纯结构预检（kind、名称字符集与字节
  上限、Add 非零 delta），与 `Apply` 共享同一 `validateOp`，不读状态。
- `stats.go` — `Stats`：读锁下的线性一致摘要（generation、nextRevision、
  账户数）。
- `clone.go` — `Clone`：读锁下逐条复制 map，保留逻辑时钟；新旧对象不共享
  任何内存（所有权完全隔离）。
- `preview.go` — `Preview`：在一次线性化快照上 `Clone` 出候选账本并对其
  `Apply`，返回候选 `Result`/`Snapshot`/`Stats`；接收方的状态、generation、
  revision 与逻辑时钟均不变，错误及优先级与同状态 `Apply` 完全一致，失败时
  全部返回零值。

## 索引与排序

主索引为哈希 map（O(1) 点查）；`Snapshot` 按名称排序，`Top` 按值降序、
名称升序排序，均在锁内物化为新切片后返回，返回切片与内部状态隔离。

## 候选事务与所有权

`Preview`/`Clone` 产出的候选账本拥有独立的 map 与锁，是深拷贝：对候选的
任何写入都不会别名原对象。所有公开方法可并发调用；返回值均为按值复制的
新切片。

## 复杂度

- `Apply` / `Preview`：O(n + a)，n 为批次 op 数，a 为当前账户数（语义预检
  需复制值视图）。
- `ValidateBatch`：O(n)，无状态访问。
- `Snapshot` / `Top`：O(a log a)；`Stats`：O(1)；`Clone`：O(a)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
