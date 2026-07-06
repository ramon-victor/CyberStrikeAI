package project

import "strings"

// VisionImageSectionMarker is the image analysis section title (consistent with AppendVisionImageAnalysisIfReady injection).
const VisionImageSectionMarker = "## Image Analysis"

// VisionImageAnalysisSection is the image analysis prompt shared by single/multi-agent (analyze_image; context only retains text summary).
func VisionImageAnalysisSection() string {
	var b strings.Builder
	b.WriteString(VisionImageSectionMarker)
	b.WriteString("\n\n")
	b.WriteString("- When encountering image files (screenshots, captchas, login pages, report figures), if the `analyze_image` tool is available, pass the file path on the server for analysis.\n")
	b.WriteString("- Do not use `read_file` on binary images expecting to understand content; the path in user messages marked \"📎 xxx.png: /path\" is exactly the path you can pass to `analyze_image`.\n")
	b.WriteString("- For captchas: if saved as a local image from a page or API (e.g., `captcha.png`), use `analyze_image` with the question clearly stating \"output only the captcha characters\"; if recognition fails, refresh the captcha, re-save, and retry; do not expect single-shot success for complex sliders or behavioral captchas.\n")
	b.WriteString("- When delegating to a sub-agent, if the subtask involves captcha or screenshot recognition, write the image path and expected output format in the task description.\n")
	return b.String()
}

// AppendVisionImageAnalysisIfReady 仅在 vision.enabled 且 model 已配置时追加图片分析提示。
func AppendVisionImageAnalysisIfReady(base string, visionReady bool) string {
	if !visionReady {
		return base
	}
	return AppendSystemPromptBlock(base, VisionImageAnalysisSection())
}
