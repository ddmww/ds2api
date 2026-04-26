'use strict';

const providerMultipliers = {
  gemini: { Word: 1.15, Number: 2.8, CJK: 0.68, Symbol: 0.38, MathSymbol: 1.05, URLDelim: 1.2, AtSign: 2.5, Emoji: 1.08, Newline: 1.15, Space: 0.2, BasePad: 0 },
  claude: { Word: 1.13, Number: 1.63, CJK: 1.21, Symbol: 0.4, MathSymbol: 4.52, URLDelim: 1.26, AtSign: 2.82, Emoji: 2.6, Newline: 0.89, Space: 0.39, BasePad: 0 },
  openai: { Word: 1.02, Number: 1.55, CJK: 0.85, Symbol: 0.4, MathSymbol: 2.68, URLDelim: 1.0, AtSign: 2.0, Emoji: 2.12, Newline: 0.5, Space: 0.42, BasePad: 0 },
};

function buildUsage(prompt, thinking, output, outputTokens = 0, providedPromptTokens = 0, model = '') {
  const reasoningTokens = estimateTokens(thinking, model);
  const completionTokens = estimateTokens(output, model);

  const finalPromptTokens = Number.isFinite(providedPromptTokens) && providedPromptTokens > 0 ? Math.trunc(providedPromptTokens) : estimateTokens(prompt, model);

  const overriddenCompletionTokens = Number.isFinite(outputTokens) && outputTokens > 0 ? Math.trunc(outputTokens) : 0;
  const finalCompletionTokens = overriddenCompletionTokens > 0 ? overriddenCompletionTokens : reasoningTokens + completionTokens;
  return {
    prompt_tokens: finalPromptTokens,
    completion_tokens: finalCompletionTokens,
    total_tokens: finalPromptTokens + finalCompletionTokens,
    prompt_tokens_details: {
      cached_tokens: 0,
      text_tokens: finalPromptTokens,
      audio_tokens: 0,
      image_tokens: 0,
    },
    completion_tokens_details: {
      text_tokens: Math.max(finalCompletionTokens - reasoningTokens, 0),
      audio_tokens: 0,
      reasoning_tokens: reasoningTokens,
    },
  };
}

function estimateTokens(text, model = '') {
  const t = asTokenString(text);
  if (!t) {
    return 0;
  }

  const m = providerMultipliers[providerForModel(model)] || providerMultipliers.openai;
  const wordNone = 0;
  const wordLatin = 1;
  const wordNumber = 2;
  let currentWordType = wordNone;
  let count = 0;

  for (const ch of Array.from(t)) {
    if (/\s/u.test(ch)) {
      currentWordType = wordNone;
      count += (ch === '\n' || ch === '\t') ? m.Newline : m.Space;
      continue;
    }
    if (isCJK(ch)) {
      currentWordType = wordNone;
      count += m.CJK;
      continue;
    }
    if (isEmoji(ch)) {
      currentWordType = wordNone;
      count += m.Emoji;
      continue;
    }
    if (isLatinOrNumber(ch)) {
      const nextType = /\p{Number}/u.test(ch) ? wordNumber : wordLatin;
      if (currentWordType === wordNone || currentWordType !== nextType) {
        count += nextType === wordNumber ? m.Number : m.Word;
        currentWordType = nextType;
      }
      continue;
    }
    currentWordType = wordNone;
    if (isMathSymbol(ch)) {
      count += m.MathSymbol;
    } else if (ch === '@') {
      count += m.AtSign;
    } else if (isURLDelim(ch)) {
      count += m.URLDelim;
    } else {
      count += m.Symbol;
    }
  }

  return Math.ceil(count) + m.BasePad;
}

function providerForModel(model = '') {
  const name = String(model || '').trim().toLowerCase();
  if (name.includes('gemini')) return 'gemini';
  if (name.includes('claude')) return 'claude';
  return 'openai';
}

function isCJK(ch) {
  const cp = ch.codePointAt(0);
  return /\p{Script=Han}/u.test(ch) || (cp >= 0x3040 && cp <= 0x30FF) || (cp >= 0xAC00 && cp <= 0xD7A3);
}

function isLatinOrNumber(ch) {
  return /\p{Letter}|\p{Number}/u.test(ch);
}

function isEmoji(ch) {
  const cp = ch.codePointAt(0);
  return (cp >= 0x1F300 && cp <= 0x1F9FF) ||
    (cp >= 0x2600 && cp <= 0x26FF) ||
    (cp >= 0x2700 && cp <= 0x27BF) ||
    (cp >= 0x1F600 && cp <= 0x1F64F) ||
    (cp >= 0x1F900 && cp <= 0x1F9FF) ||
    (cp >= 0x1FA00 && cp <= 0x1FAFF);
}

function isMathSymbol(ch) {
  const cp = ch.codePointAt(0);
  const mathSymbols = '∑∫∂√∞≤≥≠≈±×÷∈∉∋∌⊂⊃⊆⊇∪∩∧∨¬∀∃∄∅∆∇∝∟∠∡∢°′″‴⁺⁻⁼⁽⁾ⁿ₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎²³¹⁴⁵⁶⁷⁸⁹⁰';
  return mathSymbols.includes(ch) ||
    (cp >= 0x2200 && cp <= 0x22FF) ||
    (cp >= 0x2A00 && cp <= 0x2AFF) ||
    (cp >= 0x1D400 && cp <= 0x1D7FF);
}

function isURLDelim(ch) {
  return '/:?&=;#%'.includes(ch);
}

function asTokenString(v) {
  if (typeof v === 'string') {
    return v;
  }
  if (Array.isArray(v)) {
    return asTokenString(v[0]);
  }
  if (v == null) {
    return '';
  }
  return String(v);
}

module.exports = {
  buildUsage,
  estimateTokens,
};
