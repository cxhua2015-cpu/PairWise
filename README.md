# resourceledger097

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：核心状态为 `map[string]Account`，按名称 O(1) 定位账户。`Top` 与 `Snapshot`
在读取时把 map 拷贝为切片后排序（分别按值降序/名称升序、按名称升序），不维护持久有序
索引——账户数有限且写路径因此保持 O(1)，排序成本只发生在读取方。

**候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），不触碰
状态；随后在互斥锁内把账户表克隆为候选 map，按输入顺序在其上执行 Add/Set/Delete，
用局部变量推进 revision。任一步失败（`ErrNotFound`/`ErrValue`/`ErrCapacity`）直接丢弃
候选，账本、generation、revision 全部不变，实现整体回滚；全部成功且批次末账户数不超
`MaxAccounts` 时才一次性提交，generation 恰好加一。溢出在算术前用边界比较检测
（避免 `MinInt64` 取负溢出），绝对值上限同样以无溢出方式判断。

**所有权**：所有公开方法由一把 `sync.Mutex` 保护，可并发调用。`Top`/`Snapshot`/`Result.Changed`
返回的切片均为新建拷贝，调用方修改不会影响内部状态；内部 `Account` 为值类型，无共享指针。

**复杂度**（n = 账户数，b = 批次内 op 数）：
- `Apply`：校验 O(b)，执行 O(b)，候选克隆 O(n)，提交 O(1)。
- `Top(k)`：O(n log n) 排序，返回前 k 个。
- `Snapshot`：O(n log n) 按名称排序。
- 空间：O(n)，候选事务额外 O(n) 临时空间。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
