package session

import "sort"

// sortMessagesByTimeID 按 CreatedAt 再 ID 升序对消息排序。
func sortMessagesByTimeID(msgs []SessionMessage) {
	sort.SliceStable(msgs, func(i, j int) bool {
		if !msgs[i].CreatedAt.Equal(msgs[j].CreatedAt) {
			return msgs[i].CreatedAt.Before(msgs[j].CreatedAt)
		}
		return msgs[i].ID < msgs[j].ID
	})
}

// deepCopyMessages 对每条 SessionMessage 做深拷贝（ToolCalls/Metadata 独立）。
func deepCopyMessages(msgs []SessionMessage) []SessionMessage {
	if msgs == nil {
		return []SessionMessage{}
	}
	out := make([]SessionMessage, len(msgs))
	for i, m := range msgs {
		out[i] = m.clone()
	}
	return out
}

// containsString 切片包含判断。
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
