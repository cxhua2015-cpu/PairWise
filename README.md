# balanceledger337

并发安全的内存型余额账本，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`（名称 → 账户），按名称 O(1) 查找/插入/删除。
- `Top` 与 `Snapshot` 不维护有序索引，而是在读路径上对当前账户快照排序：
  `Top` 按数值降序、名称升序；`Snapshot` 按名称升序。账户数为 N 时排序复杂度 O(N log N)。
  这样写路径保持 O(1) 摊销，且无需维护额外的有序结构。

### 候选事务（candidate transaction）
- `Apply` 先做完整结构校验（kind、名称字符集与字节上限），不触碰状态。
- 通过校验后在持写锁状态下克隆当前 map 得到候选状态，按输入顺序在其上执行
  Add/Set/Delete：Add/Set 从 `nextRevision` 起分配连续 revision；算术前检测
  int64 溢出并执行 `MaxAbsValue` 绝对值上限（违反返回 `ErrValue`）；
  Delete 缺失账户返回 `ErrNotFound`。
- 最终账户容量仅在批次末对候选状态检查（超过返回 `ErrCapacity`）。
- 任一步失败直接丢弃候选状态，账本保持原样（整体回滚）；全部成功才用候选
  map 原子替换内部状态，且 generation 只增加一次（空批次不增加）。

### 所有权与并发
- 所有公开方法并发安全：写操作持 `sync.Mutex` 写锁，`Top`/`Snapshot` 持读锁。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为每次调用新建的
  副本，与内部状态完全隔离；调用方修改返回值不影响账本。
- `Account` 为值类型，map 中存值不存指针，克隆与替换均为值拷贝，无共享别名。

### 复杂度
- `Apply`：O(K + N)，K 为批内操作数，N 为当前账户数（候选克隆）。
- `Top` / `Snapshot`：O(N log N)（排序），返回切片 O(N) 额外空间。
- 空间：O(N)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
