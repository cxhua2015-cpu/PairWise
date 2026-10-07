# balanceledger392

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，失败整体回滚。

## 索引

- 主存储：`map[string]Account`，按账户名 O(1) 定位。
- `Top` 与 `Snapshot` 不加额外索引，按需对账户快照排序（Top：数值降序、
  名称升序；Snapshot：名称升序），避免写路径维护有序结构的成本。

## 候选事务（candidate transaction）

`Apply` 先做完整结构校验（kind、名称字符集与字节上限），不读取状态；随后
在互斥锁内把当前 `accounts` 复制为候选 map，在副本上顺序执行全部操作
（Add/Set 分配连续 revision，算术前检测 int64 溢出并执行绝对值上限），
批次末才检查账户容量。任一步失败直接丢弃候选副本，实现整体回滚；全部
成功才用候选副本替换主存储，且非空成功批次 generation 只增加一次。

## 所有权

- 锁内所有可变状态仅由 `Ledger` 持有；`Result.Changed`、`Top`、`Snapshot`
  返回的切片均为新建副本，调用方修改不影响内部状态。
- 单个 `sync.Mutex` 串行化所有读写，保证公开方法并发安全。

## 复杂度

- `Apply`：O(A + B)，A 为账户数（复制候选），B 为批内操作数。
- `Top`：O(A log A) 排序后取前 n。
- `Snapshot`：O(A log A) 排序。
- 空间：O(A + B)。

## 测试

`contract_test.go` 为契约测试；`extra_test.go` 覆盖非法 Options/名称、
int64 溢出、绝对值上限、容量回滚、generation 语义、Top 排序、返回切片
隔离及混合并发（含 `-race`）。

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
