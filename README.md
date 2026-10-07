# metacatalog401

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `servicecatalog.go` — 核心事务引擎：`Store`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与名称规则，和 `Apply` 共享同一套结构语义。
- `stats.go` — 线性一致的状态统计：`Stats` 在读锁下观察单一已提交 generation。
- `clone.go` — 保留逻辑时钟（generation / next revision）且所有权完全隔离的深拷贝。

## 索引（Index）

`Store` 以 `map[string]Record` 作为主索引，键为记录名，值内嵌 `Revision` 与 `Value` 副本。`generation`、`nextRevision`、`totalValueBytes` 作为派生状态与索引一同在 `sync.RWMutex` 下维护：写路径独占锁，读路径（`Get`/`Snapshot`/`Stats`/`Clone`）共享读锁，因此统计与快照永远对应某个已提交的 generation，是线性一致的。

## 候选事务（Candidate transaction）

`Apply` 先做完整结构校验（不读状态），再克隆当前索引得到候选事务，在候选上按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配且要求目标存在。记录数与 Value 总字节容量只在批次末检查；任何失败直接丢弃候选，已提交的索引、generation 与 revision 完全不变（天然回滚）。全部检查通过后一次性交换索引并推进时钟，非空成功批次 generation 恰好加一。

## 所有权（Ownership）

所有跨越 API 边界的 `[]byte` 都做防御性拷贝：Put 时拷贝入站 Value，`Get`/`Snapshot`/`Result.Changed` 返回出站深拷贝，`Clone` 拷贝全部记录。调用方对返回切片的修改不会污染目录，目录后续变更也不会影响已返回的切片；克隆体与原Store不共享任何可变内存，可独立并发使用。

## 复杂度（Complexity）

设 n 为当前记录数、k 为批次数、v 为涉及的 Value 总字节数：

- `Apply`：O(n + k + v)（克隆索引 + 顺序执行 + 拷贝），容量检查 O(n)。
- `Get`：O(1) 索引查找 + O(|value|) 拷贝。
- `Snapshot`：O(n log n) 排序 + O(v) 拷贝。
- `Stats`：O(1)。`Clone`：O(n + v)。
- `ValidateBatch`：O(k) 纯结构校验，不触碰状态。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
