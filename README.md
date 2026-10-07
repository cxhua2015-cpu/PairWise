# balanceledger322

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护有序索引：`Top` 在读锁内对当前账户快照排序（值降序、名称升序），`Snapshot` 按名称升序排序。账户数受 `MaxAccounts` 上限约束，排序成本有界。

### 候选事务（candidate transaction）
- `Apply` 先做整批结构校验（kind、名称字符集与字节长度），不触碰状态。
- 随后在账户表的**拷贝**上按输入顺序执行 Add/Set/Delete：算术前检测 int64 溢出并执行 `±MaxAbsValue` 上限；Add/Set 分配连续 revision；账户容量仅在批次末检查。
- 任一步失败即丢弃候选，整体回滚——`generation`、`nextRevision`、账户表均不变。全部成功才一次性提交，`generation` 恰好 +1。

### 所有权与并发
- 所有公开方法通过一把 `sync.RWMutex` 保护：`Apply` 持写锁，`Top`/`Snapshot` 持读锁，可多读者并发。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的副本，调用方修改不影响内部状态；`Account` 为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(B + A)，B 为批内 op 数，A 为当前账户数（候选拷贝）。
- `Top`：O(A log A)；`Snapshot`：O(A log A)；`New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
