export default function HistorySplitSection({ t, form, setForm }) {
    return (
        <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <div className="space-y-1">
                <h3 className="font-semibold">{t('settings.historySplitTitle')}</h3>
                <p className="text-sm text-muted-foreground">{t('settings.historySplitDesc')}</p>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <label className="flex items-start gap-3 rounded-lg border border-border bg-background/60 p-4">
                    <input
                        type="checkbox"
                        checked
                        disabled
                        readOnly
                        className="mt-1 h-4 w-4 rounded border-border disabled:opacity-70"
                    />
                    <div className="space-y-1">
                        <span className="text-sm font-medium block">{t('settings.historySplitEnabled')}</span>
                        <span className="text-xs text-muted-foreground block">{t('settings.historySplitEnabledDesc')}</span>
                    </div>
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.historySplitTriggerAfterTurns')}</span>
                    <input
                        type="number"
                        min={1}
                        max={1000}
                        value={form.history_split.trigger_after_turns}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            history_split: {
                                ...prev.history_split,
                                trigger_after_turns: Number(e.target.value || 1),
                            },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                    <p className="text-xs text-muted-foreground">{t('settings.historySplitTriggerHelp')}</p>
                </label>
                <label className="flex items-start gap-3 rounded-lg border border-border bg-background/60 p-4">
                    <input
                        type="checkbox"
                        checked={Boolean(form.history_split.use_file ?? true)}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            history_split: {
                                ...prev.history_split,
                                use_file: e.target.checked,
                            },
                        }))}
                        className="mt-1 h-4 w-4 rounded border-border"
                    />
                    <div className="space-y-1">
                        <span className="text-sm font-medium block">{t('settings.historySplitUseFile')}</span>
                        <span className="text-xs text-muted-foreground block">{t('settings.historySplitUseFileDesc')}</span>
                    </div>
                </label>
            </div>
        </div>
    )
}
