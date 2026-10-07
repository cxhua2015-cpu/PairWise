# readyqueue320

并发安全的内存型“就绪优先队列”，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 使用

```go
q, _ := readyqueue320.New(readyqueue320.Options{MaxItems: 1024, MaxIDBytes: 64})
r, _ := q.Apply(readyqueue320.Batch{Now: 10, Ops: []readyqueue320.Op{
    {Kind: readyqueue320.Enqueue, ID: "job-1", Priority: 5, ReadyAt: 3},
}})
items, _ := q.Pop(10, 1)
snap := q.Snapshot()
```

## 实现说明

**索引**：队列内部以 `map[string]Item` 为主索引，按 ID O(1) 定位，用于
Enqueue 的存在性检查与 Cancel 的删除。规范顺序（Priority 降序、ReadyAt 升序、
ID 升序）不维护额外有序结构，而是在 `Pop`/`Snapshot` 时对候选集即时排序，
以换取写入路径的极简与回滚的低成本。

**候选事务**：`Apply` 先在锁外做整批结构校验（kind、ID 字符集与字节上限、
ReadyAt 非负），再在单把互斥锁内顺序执行 Enqueue/Cancel。每次变更记录一条
undo（Enqueue 记新增，Cancel 记被删前值）；任一步失败（`ErrExists`、
`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时逆序回放 undo，并回退
`nextRevision`，时间 `now` 与 `generation` 只在整批成功后才提交，因此失败
批次对外完全不可见。空批次不增加 generation。

**所有权**：`Queue` 内部状态（`now`、`generation`、`nextRevision`、`items`）
全部由一把 `sync.Mutex` 保护，所有公开方法可并发调用。`Pop` 与 `Snapshot`
返回的切片均为新建并拷贝的 `Item` 值切片，调用方对返回值的任何修改都不会
影响队列内部状态。

**复杂度**（n = 队列中元素数，b = 批次大小，k = 就绪候选数，m = 弹出数量）：
- `New`：O(1)。
- `Apply`：O(b) 均摊——校验与逐条 map 操作，回滚同为 O(b)。
- `Pop`：O(n + k log k)——扫描筛选就绪项，排序后取前 m 个并删除。
- `Snapshot`：O(n log n)——拷贝并排序全部元素。
