package store

import "encoding/json"

// JSONMergePatch implements RFC 7396. Given a target document and a
// patch, produce the merged result. Object keys present in patch with a
// null value are deleted; nested objects merge recursively; non-object
// patches replace the target wholesale.
//
// Both inputs must be valid JSON (caller's responsibility).
//
// Ported verbatim from internal/data/operations.go to keep behavior
// identical — agents have been writing against this semantics already
// and we don't want to change merge behavior in the same release as
// the storage swap.
func JSONMergePatch(original, patch []byte) []byte {
	var origMap map[string]any
	var patchMap map[string]any

	if err := json.Unmarshal(patch, &patchMap); err != nil {
		// Patch is not an object — replaces wholesale.
		return patch
	}

	if err := json.Unmarshal(original, &origMap); err != nil {
		// Original is not an object — patch replaces wholesale.
		result, _ := json.Marshal(patchMap)
		return result
	}

	for k, v := range patchMap {
		if v == nil {
			delete(origMap, k)
			continue
		}
		origVal, exists := origMap[k]
		if exists {
			origBytes, _ := json.Marshal(origVal)
			patchBytes, _ := json.Marshal(v)
			merged := JSONMergePatch(origBytes, patchBytes)
			var mergedVal any
			_ = json.Unmarshal(merged, &mergedVal)
			origMap[k] = mergedVal
		} else {
			origMap[k] = v
		}
	}

	result, _ := json.Marshal(origMap)
	return result
}
