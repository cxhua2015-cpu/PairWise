# metacatalog256

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete，Put 分配连续 revision，Delete 不分配；失败回滚全部状态与逻辑时钟。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot`，索引与候选事务提交。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义
  （名称字符集/长度、kind 合法、Put 值长上限、Delete 必须携带 nil Value），只读不可变的
  Options，绝不触碰状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁内一次性汇总 generation、next revision、
  记录数与 Value 总字节，始终对应两个已提交事务之间的单一一致时点。
- `clone.go` — 所有权隔离的深拷贝：`Clone` 复制全部记录与逻辑时钟（generation、
  next revision），副本与原件不共享任何可变内存，可独立并发演化。

## 索引

记录存储在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；`Snapshot`/`Changed`
在读取后按名称排序输出。单把 `sync.RWMutex` 保护全部状态：写事务独占，读路径
（`Get`/`Snapshot`/`Stats`/`Clone`）共享读锁。

## 候选事务

`Apply` 先执行完整结构预检（不读状态），再在提交状态的私有副本（candidate map）
上按顺序应用操作：Put 从当前 next revision 起连续分配，Delete 校验存在性但不分配。
记录数与 Value 总字节容量只在批次末检查。任一步失败直接丢弃候选，已提交的
records、generation、revision 均不变；成功时一次性换入候选，非空批次 generation
只增加一次，空批次完全不变。

## 所有权

所有跨边界的数据都被复制：Put 的 Value 在写入时克隆，`Get`/`Snapshot`/`Result.Changed`
返回深拷贝，`Clone` 逐条复制。调用方对返回切片的任何修改、以及随后对入参切片的
修改，都不会影响目录内部状态。

## 复杂度

- `Get`：O(1)；`Stats`：O(n)。
- `Snapshot`/`Clone`：O(n + k log k)，k 为记录数（排序），深拷贝字节 O(V)。
- `Apply`：结构预检 O(m)，候选复制 O(n)，应用 O(m)，末次容量汇总 O(n)，
  Changed 排序 O(m log m)；m 为批次内操作数，n 为当前记录数，V 为 Value 总字节。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
