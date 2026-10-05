# Config stack specification

`New` requires positive `MaxLayers`, `MaxEntries`, `MaxNameBytes`, `MaxKeyBytes`, `MaxValueBytes`, and `MaxTotalValueBytes`. Names and keys use nonempty ASCII letters, digits, dot, underscore, slash or hyphen within their byte limits.

`Apply` structurally validates the entire batch before reading state. AddLayer requires Name and zero Position fields other than Position in range 0..current-independent `MaxLayers-1`; its actual insertion position must be at most the candidate layer count when executed. RemoveLayer requires only Name. Set requires Name, Key and a non-nil Value within MaxValueBytes. Delete requires Name and Key with nil Value. Move requires Name and nonnegative Position with no Key/Value. Extra fields or unknown kinds return `ErrInvalidInput`.

Operations execute sequentially on an isolated candidate. AddLayer requires an absent name and inserts at Position (0 is highest priority). RemoveLayer requires an existing layer and removes its entries. Set requires an existing layer and inserts/replaces the key, allocating one revision. Delete requires an existing key. Move requires an existing layer and moves it to Position in the layer list after temporarily removing it; Position may equal the resulting length (move to bottom). Add/Remove/Move do not allocate revisions.

Final layer count, entry count and total live value bytes are checked only after all operations. Any failure rolls back everything. A successful nonempty batch increments generation once. Result.Changed contains surviving entries touched by Set, sorted by layer priority then key, without duplicates. Result.Revision is the latest committed revision or zero.

`Resolve(key)` validates key and scans layers from highest to lowest priority. It returns a deep copy plus Found, or Found=false. Snapshot preserves layer order, sorts each layer's entries by key, and deep-copies values. All methods are concurrency-safe.
