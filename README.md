# windowlimit

并发安全、显式时间驱动的内存滑动窗口限流器（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引结构

限流器内部以 `map[string][]Event` 维护每个 key 的存活事件。事件按提交顺序追加，天然按 `(At, Revision)` 有序；`Snapshot` 对 key 做字典序排序、对事件按 `(At, Revision)` 稳定排序，并返回深拷贝切片，调用方修改不影响内部状态。

## 事务模型

`Check` 分为三个阶段：

1. **校验**：先检查 `Now >= 0`、key 字符集/长度、`0 < Units <= Limit`；结构错误返回 `ErrInvalidInput`，优先于时间错误。随后检查时间单调性，倒退返回 `ErrTime`。
2. **候选执行**：在隔离的候选状态上先淘汰 `At <= Now-Window` 的事件（窗口左边界排除），再按输入顺序逐个决策。允许的请求追加事件并分配一个 revision；拒绝的请求不写事件、不消耗 revision。
3. **提交或回滚**：全部请求执行完后才检查容量（去重非空 key 数 `MaxKeys`、每 key 事件数 `MaxEventsPerKey`）。容量失败返回 `ErrCapacity`，淘汰、时间、generation、revision 全部回滚，状态与调用前完全一致。

任何成功的 `Check`（包括空批次）都会推进时间并执行淘汰。淘汰删除了事件或至少一个请求被允许时，`Generation` 恰好加一；无变化的空批次不增加。`Result.Revision` 为最新已提交 revision，无事件时为零。

## 窗口边界

窗口为 `(Now-Window, Now]`：`At <= Now-Window` 的事件在候选阶段被淘汰，`At == Now-Window` 同样被排除；`At > Now-Window` 的事件计入用量。

## 容量

- `MaxKeys`：窗口内去重后的非空 key 数上限。
- `MaxEventsPerKey`：每个 key 的存活事件数上限。
- 容量只在批次末尾检查，因此淘汰可以先释放空间，使本批次合法。

## 并发

所有公开方法通过单个互斥锁串行化，`Check` 与 `Snapshot` 可安全并发调用。

## 复杂度

设批次大小为 B、存活事件总数为 N、key 数为 K：

- `Check`：校验 O(B)，候选复制与淘汰 O(N)，决策 O(B)，容量检查 O(K)，总体 O(N + B)。
- `Snapshot`：排序 O(K log K + N log N)，外加深拷贝 O(N)。
- 空间：O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
