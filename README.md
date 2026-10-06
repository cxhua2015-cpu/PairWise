# metacatalog241

并发安全的内存型“元数据目录 241”：原子批次执行 Put/Delete，仅依赖标准库（Go 1.22+）。

## 语义

- 批次按输入顺序执行；Put 分配连续 revision，Delete 不分配。
- 完整结构校验先于任何状态读取；记录数与 Value 总字节容量只在批次末检查。
- 任何失败回滚全部状态、generation 与 revision；非空成功批次 generation 只加一，空批次不变。
- `Get`/`Snapshot` 深拷贝 Value，`Snapshot` 按名称排序；所有公开方法并发安全。

## 架构

- **索引**：`Store` 持有 `map[string]entry`（name → value+revision），外加 `generation` 与 `nextRevision` 逻辑时钟；单把 `sync.RWMutex` 保护，读路径（`Get`/`Snapshot`/`Stats`/`Clone`）走 `RLock`。
- **候选事务**：`Apply` 先调用与 `ValidateBatch` 共享的无副作用结构预检，再在写锁内浅拷贝索引作为候选 map，顺序应用操作（Put 深拷贝 Value 后替换条目，绝不原地修改）。容量在批次末对候选集检查；提交即指针交换，失败直接丢弃候选，天然回滚。
- **所有权**：写入时拷贝调用方 Value，读出时（`Get`/`Snapshot`/`Result.Changed`/`Clone`）再次深拷贝，内外互不别名；`Clone` 复制逻辑时钟与全部记录，生成完全独立的 Store。
- **复杂度**：结构校验 O(batch)；`Apply` 为 O(n + B)，n 为当前记录数（候选拷贝）、B 为批大小；`Get` O(1)；`Snapshot`/`Clone`/`Stats` O(n)；`Snapshot` 与 `Changed` 排序 O(n log n)。

## 文件

- `servicecatalog.go` — 类型、错误值、核心事务引擎（`New`/`Apply`/`Get`/`Snapshot`）
- `validation.go` — 共享结构预检（`ValidateBatch`）
- `stats.go` — 线性一致统计（`Stats`）
- `clone.go` — 保留逻辑时钟的独立深拷贝（`Clone`）

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
