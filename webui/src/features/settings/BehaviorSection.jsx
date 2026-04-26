export default function BehaviorSection({ t, form, setForm }) {
    return (
        <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <h3 className="font-semibold">{t('settings.behaviorTitle')}</h3>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.responsesTTL')}</span>
                    <input
                        type="number"
                        min={30}
                        value={form.responses.store_ttl_seconds}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            responses: { ...prev.responses, store_ttl_seconds: Number(e.target.value || 30) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.embeddingsProvider')}</span>
                    <input
                        type="text"
                        value={form.embeddings.provider}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            embeddings: { ...prev.embeddings, provider: e.target.value },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
            </div>
            <div className="border-t border-border pt-4 space-y-4">
                <div className="flex items-center justify-between gap-4">
                    <div>
                        <h4 className="text-sm font-medium">{t('settings.truncationAutoContinueTitle')}</h4>
                        <p className="text-xs text-muted-foreground mt-1">{t('settings.truncationAutoContinueDesc')}</p>
                    </div>
                    <button
                        type="button"
                        role="switch"
                        aria-checked={Boolean(form.truncation_auto_continue?.enabled)}
                        onClick={() => setForm((prev) => ({
                            ...prev,
                            truncation_auto_continue: {
                                ...prev.truncation_auto_continue,
                                enabled: !Boolean(prev.truncation_auto_continue?.enabled),
                            },
                        }))}
                        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                            form.truncation_auto_continue?.enabled ? 'bg-primary' : 'bg-muted'
                        }`}
                    >
                        <span
                            className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                                form.truncation_auto_continue?.enabled ? 'translate-x-6' : 'translate-x-1'
                            }`}
                        />
                    </button>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.truncationMaxRounds')}</span>
                        <input
                            type="number"
                            min={1}
                            max={8}
                            value={form.truncation_auto_continue?.max_rounds || 2}
                            onChange={(e) => setForm((prev) => ({
                                ...prev,
                                truncation_auto_continue: {
                                    ...prev.truncation_auto_continue,
                                    max_rounds: Number(e.target.value || 2),
                                },
                            }))}
                            className="w-full bg-background border border-border rounded-lg px-3 py-2"
                        />
                    </label>
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.truncationMinChars')}</span>
                        <input
                            type="number"
                            min={50}
                            max={10000}
                            value={form.truncation_auto_continue?.min_chars || 120}
                            onChange={(e) => setForm((prev) => ({
                                ...prev,
                                truncation_auto_continue: {
                                    ...prev.truncation_auto_continue,
                                    min_chars: Number(e.target.value || 120),
                                },
                            }))}
                            className="w-full bg-background border border-border rounded-lg px-3 py-2"
                        />
                    </label>
                    <div className="flex items-center justify-between gap-4 rounded-lg border border-border px-3 py-2">
                        <span className="text-sm text-muted-foreground">{t('settings.truncationPlainText')}</span>
                        <button
                            type="button"
                            role="switch"
                            aria-checked={Boolean(form.truncation_auto_continue?.plain_text)}
                            onClick={() => setForm((prev) => ({
                                ...prev,
                                truncation_auto_continue: {
                                    ...prev.truncation_auto_continue,
                                    plain_text: !Boolean(prev.truncation_auto_continue?.plain_text),
                                },
                            }))}
                            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                                form.truncation_auto_continue?.plain_text ? 'bg-primary' : 'bg-muted'
                            }`}
                        >
                            <span
                                className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                                    form.truncation_auto_continue?.plain_text ? 'translate-x-6' : 'translate-x-1'
                                }`}
                            />
                        </button>
                    </div>
                </div>
            </div>
            <div className="border-t border-border pt-4 space-y-4">
                <div className="flex items-center justify-between gap-4">
                    <div>
                        <h4 className="text-sm font-medium">{t('settings.upstreamBlockerTitle')}</h4>
                        <p className="text-xs text-muted-foreground mt-1">{t('settings.upstreamBlockerDesc')}</p>
                    </div>
                    <button
                        type="button"
                        role="switch"
                        aria-checked={Boolean(form.upstream_blocker?.enabled)}
                        onClick={() => setForm((prev) => ({
                            ...prev,
                            upstream_blocker: {
                                ...prev.upstream_blocker,
                                enabled: !Boolean(prev.upstream_blocker?.enabled),
                            },
                        }))}
                        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                            form.upstream_blocker?.enabled ? 'bg-primary' : 'bg-muted'
                        }`}
                    >
                        <span
                            className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                                form.upstream_blocker?.enabled ? 'translate-x-6' : 'translate-x-1'
                            }`}
                        />
                    </button>
                </div>
                <label className="text-sm space-y-2 block">
                    <span className="text-muted-foreground">{t('settings.upstreamBlockerKeywords')}</span>
                    <textarea
                        rows={5}
                        value={form.upstream_blocker?.keywords_text || ''}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            upstream_blocker: { ...prev.upstream_blocker, keywords_text: e.target.value },
                        }))}
                        placeholder="sorry&#10;我无法&#10;拒绝此请求"
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 font-mono text-xs"
                    />
                </label>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className="text-sm space-y-2">
                        <span className="text-muted-foreground">{t('settings.upstreamBlockerMessage')}</span>
                        <input
                            type="text"
                            value={form.upstream_blocker?.message || ''}
                            onChange={(e) => setForm((prev) => ({
                                ...prev,
                                upstream_blocker: { ...prev.upstream_blocker, message: e.target.value },
                            }))}
                            className="w-full bg-background border border-border rounded-lg px-3 py-2"
                        />
                    </label>
                    <div className="flex items-center justify-between gap-4 rounded-lg border border-border px-3 py-2">
                        <span className="text-sm text-muted-foreground">{t('settings.upstreamBlockerCaseSensitive')}</span>
                        <button
                            type="button"
                            role="switch"
                            aria-checked={Boolean(form.upstream_blocker?.case_sensitive)}
                            onClick={() => setForm((prev) => ({
                                ...prev,
                                upstream_blocker: {
                                    ...prev.upstream_blocker,
                                    case_sensitive: !Boolean(prev.upstream_blocker?.case_sensitive),
                                },
                            }))}
                            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                                form.upstream_blocker?.case_sensitive ? 'bg-primary' : 'bg-muted'
                            }`}
                        >
                            <span
                                className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                                    form.upstream_blocker?.case_sensitive ? 'translate-x-6' : 'translate-x-1'
                                }`}
                            />
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
