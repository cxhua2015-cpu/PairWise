# resourceledger147

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。规范见 `SPEC.md`。

## 架构（三层联动）

- **状态引擎 `creditpool.go`**：`Ledger` 持有事务性数据与快照。
- **策略层 `policy.go`**：`Policy` 独立同步，维护可原子替换的 actor 白名单与单批操作数上限。
- **协调层 `coordinator.go`**：`Coordinator` 先授权再调用引擎，并为成功、拒绝、引擎失败分配连续审计序号。

## 索引

核心状态为 `map[string]Account` 哈希索引，按名 O(1) 定位账户。`Top` 与 `Snapshot` 在读取时拷贝全部条目并排序，不维护额外有序结构，以保持写路径 O(1)。

## 候选事务

`Apply` 先做完整结构校验（kind、名称字符集与字节上限），不触碰状态；随后在单个互斥锁内把索引克隆为候选 map，按输入顺序执行 Add/Set/Delete：Add/Set 分配连续 revision，算术前检测 int64 溢出并执行绝对值上限，最终账户容量仅在批次末检查。任何失败直接丢弃候选 map，实现整体回滚；成功则整体换入并使 generation 恰好加一（空批次不变）。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`、`Coordinator.Decisions`）均为新分配的拷贝，与内部存储不共享内存；调用方修改返回值不影响账本。`Policy.ReplaceActors` 也会拷贝白名单。

## 复杂度

- `Apply`：O(n + a)，n 为批内操作数，a 为当前账户数（候选克隆）。
- `Top` / `Snapshot`：O(a log a)。
- `Authorize` / `ReplaceActors`：O(1) / O(k)，k 为 actor 数。
- `Coordinator.Apply`：授权 + 一次引擎调用 + O(1) 审计追加；`Decisions` 为 O(d) 拷贝。

## 并发安全

`Ledger` 用单一 `sync.Mutex` 串行化批次与读取；`Policy` 用 `sync.RWMutex` 保护可替换配置；`Coordinator` 用独立互斥锁维护单调审计日志。三层可并发调用。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
