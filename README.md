# jsonpatch

并发安全的内存 JSON Patch（RFC 6902 风格）文档存储，Go 1.22+，仅依赖标准库。公开 API 见 `jsondoc/jsondoc.go`，语义规范见 `SPEC.md`。

## 内部 JSON 表示

文档解码为 Go 原生值树：`map[string]any`（对象）、`[]any`（数组）、`string`、`json.Number`、`bool`、`nil`。解码使用 `json.Decoder.UseNumber`，因此数字保留原始词素（lexeme），不做 float64 有损转换。序列化为紧凑 JSON，对象键按字典序排序，输出确定；数字按原词素写回。

## Pointer 解析

空字符串指向根；其余必须以 `/` 开头。token 解码 `~1`→`/`、`~0`→`~`，其他 `~` 转义报 `ErrInvalidPointer`。数组下标必须是 `0` 或非零数字开头的十进制串，前导零、符号、溢出均为 `ErrInvalidIndex`；`-` 仅允许作为 `add` 的末段 token 表示追加。访问下标必须 `< len`，`add` 插入下标可等于 `len`。

## 六类操作

- `add`：根位置整体替换；对象成员创建或覆盖；数组按下标插入或 `-` 追加。
- `remove`：删除已存在的对象成员或数组元素；删除根报 `ErrRootRemoval`。
- `replace`：替换已存在的值，根替换允许。
- `copy`：深拷贝 `From` 处值，再按 add 语义写入 `Path`。
- `move`：`From == Path` 为成功 no-op；禁止移动到自身严格后代（`ErrMoveIntoChild`，含根移动到非根）；否则先删除 `From`，再对删除后的文档按 add 语义写入 `Path`（数组下标按删除后状态解释）。
- `test`：JSON 语义相等比较，对象成员顺序与数字词素无关，数字按精确数学值比较（`big.Rat`），`1`、`1.0`、`1e0` 相等；不匹配报 `ErrTestFailed`。

## 批量原子性与 revision

`Apply` 先对全部操作做结构校验（操作名、Value/From 约束、Pointer 合法性），再比较 `expectedRevision`，然后在隔离深拷贝上按序执行，最后才检查节点预算。任一步失败返回零值 `Result`，文档与 revision 不变（回滚）。成功批次只要含至少一个变更操作（add/remove/replace/move/copy）就将 revision 恰好推进 1；空批次或纯 test 批次不改变 revision。结构错误与执行错误均包装为 `*OpError`（含操作下标与路径，`Unwrap` 支持 `errors.Is`）；revision 冲突与容量超限不包装。

## 节点计数

每个对象、数组、字符串、数字、布尔、null 计 1 个节点；对象成员名不计。初始文档与每批执行后的最终状态都不得超过 `MaxNodes`；批次执行过程中的临时超限允许，只要最终状态达标。

## Payload 所有权

输入字节（初始文档、`Operation.Value`）在成功返回前完成解码拷贝，之后调用方修改输入不影响存储。`Result.Document` 与 `Snapshot.Document` 为每次调用独立序列化的全新字节切片，与内部状态及其他返回值互不影响。

## 并发与复杂度

所有公开方法可并发调用：`Apply` 持写锁串行化（天然实现 revision CAS 语义），`Snapshot` 持读锁。

设文档大小为 N 节点、批次含 K 个操作、指针深度为 D：

- 时间：`Apply` 为 O(N + K·D + K·V)，其中 N 来自每次 Apply 的深拷贝与节点计数，V 为单个操作值的大小；数组插入/删除额外 O(段长)。`Snapshot` 为 O(N)。`test` 的数字比较为 O(词素长度)。
- 空间：每次 `Apply`/`Snapshot` 额外 O(N)（隔离副本与输出字节）；存储本身常驻 O(N)。
