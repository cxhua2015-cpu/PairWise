# creditpool

并发安全的内存型信用额度池（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]*Account`，按名称 O(1) 定位账户。
- `Top` 与 `Snapshot` 不维护有序索引，而是每次调用时将账户拷贝到切片后排序：
  `Top` 按数值降序、名称升序，`Snapshot` 按名称升序。账户数受 `MaxAccounts`
  上限约束，排序成本可控，且避免在写路径上维护额外结构。

**候选事务（candidate transaction）**
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind 合法、名称字符集与字节
  上限），不读取任何状态；通过后在写锁内克隆主索引得到候选 map，账户结构体
  采用写时复制（copy-on-write），所有 Add/Set/Delete 只作用于候选。
- 溢出（int64 加法前后边界检测）与绝对值上限在每次算术前检查；账户容量
  （`MaxAccounts`）仅在批次末对最终账户集合检查一次。
- 任一步失败直接丢弃候选并返回错误，活动状态零副作用——revision 不消耗、
  generation 不增加，天然实现整体回滚。全部通过则一次性交换候选 map 提交；
  非空成功批次 generation 恰好加一，空批次不改变任何计数器。

**所有权**
- 所有公开方法返回的切片与 `Account` 值均为新分配的拷贝，调用方修改返回值
  不会影响内部状态；内部也绝不把指向活动账户的指针泄漏给调用方。
- 并发控制使用 `sync.RWMutex`：`Apply` 持写锁，`Top`/`Snapshot` 持读锁，
  读操作之间可并行。

**复杂度**（n = 账户数，b = 批次内 op 数，k = Top 参数）
- `Apply`：结构校验 O(b)；候选克隆 O(n)；执行 O(b)。总 O(n + b)。
- `Top`：O(n log n)，返回切片长度 ≤ min(k, n)。
- `Snapshot`：O(n log n)。
- 空间：O(n)，`Apply` 期间临时多一份候选索引 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
