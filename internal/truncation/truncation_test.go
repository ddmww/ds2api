package truncation

import "testing"

func TestShouldContinueUnclosedFence(t *testing.T) {
	text := "下面是代码：\n```go\nfunc main() {\n"
	if !ShouldContinue(text, true, 120) {
		t.Fatal("expected unclosed code fence to continue")
	}
}

func TestShouldContinueCompleteSentence(t *testing.T) {
	text := "这是一个完整的回答，已经正常结束。"
	if ShouldContinue(text, true, 50) {
		t.Fatal("expected complete sentence to stop")
	}
}

func TestShouldContinueLongPlainTextMidSentence(t *testing.T) {
	text := "这是一段很长的回答，用来模拟模型输出到一半被截断。" +
		"它已经超过了最小长度，而且最后一行没有任何结束标点，像是仍然在解释某个没有说完的概念"
	if !ShouldContinue(text, true, 50) {
		t.Fatal("expected long mid-sentence text to continue")
	}
}

func TestShouldContinuePlainTextDisabled(t *testing.T) {
	text := "这是一段很长的回答，用来模拟模型输出到一半被截断，最后没有结束"
	if ShouldContinue(text, false, 50) {
		t.Fatal("expected plain text heuristic to be disabled")
	}
}

func TestDeduplicateContinuationOverlap(t *testing.T) {
	existing := "第一段内容。\n第二段内容还没有写完"
	continuation := "第二段内容还没有写完，后面继续补全。"
	got := DeduplicateContinuation(existing, continuation)
	if got != "，后面继续补全。" {
		t.Fatalf("unexpected dedupe result: %q", got)
	}
}
