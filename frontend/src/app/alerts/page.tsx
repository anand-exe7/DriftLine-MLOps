"use client";

import { useState } from "react";
import { AlertOctagon, AlertTriangle, BellOff, CheckCircle2, Hash, Info, MessageSquare, Send, XCircle } from "lucide-react";
import {
  Button,
  Card,
  EmptyState,
  ErrorNote,
  Explainer,
  LiveBadge,
  PageHeader,
  StatTile,
  cx,
} from "@/components/ui";
import { go, type Alert } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { useNow, usePoll } from "@/lib/usePoll";

const SEVERITY: Record<Alert["severity"], { Icon: typeof Info; cls: string; label: string }> = {
  critical: { Icon: AlertOctagon, cls: "text-critical", label: "Critical" },
  warning: { Icon: AlertTriangle, cls: "text-serious", label: "Warning" },
  info: { Icon: Info, cls: "text-ink-2", label: "Info" },
};

const CHANNELS = [
  { id: "slack", label: "Slack", env: "SLACK_WEBHOOK_URL", Icon: Hash, how: "Incoming webhook, Block Kit message" },
  { id: "discord", label: "Discord", env: "DISCORD_WEBHOOK_URL", Icon: MessageSquare, how: "Channel webhook, colored embed" },
];

export default function AlertsPage() {
  const alerts = usePoll(() => go.alerts(200), 5000);
  const channels = usePoll(() => go.alertChannels(), 30000);
  const now = useNow(5000);
  const [sending, setSending] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string }>();

  const list = alerts.data ?? [];
  const configured = new Set(channels.data?.channels ?? []);
  const sent = list.filter((a) => a.status === "sent").length;

  const sendTest = async () => {
    setSending(true);
    setResult(undefined);
    try {
      const r = await go.testAlert();
      setResult({ ok: true, text: `Delivered to ${r.sent_to.join(" and ")}` });
    } catch (e) {
      setResult({ ok: false, text: e instanceof Error ? e.message : String(e) });
    } finally {
      setSending(false);
      alerts.refresh();
    }
  };

  return (
    <>
      <PageHeader
        title="Alerts"
        subtitle="Every notification Driftline has tried to send, and the channels it sends to."
        actions={<LiveBadge updatedAt={alerts.updatedAt} error={alerts.error} intervalLabel="5s" />}
      />

      <Explainer title="How alerting works">
        <p>
          When the drift engine finishes a check it hands a <b>drift report</b> to the alert service. If drift was detected,
          and this model version hasn&apos;t alerted within the <b>cooldown</b> window (30 min by default, so one bad hour
          doesn&apos;t send 60 messages), the report goes to every configured channel <b>in parallel</b>, so a slow Slack never
          delays Discord.
        </p>
        <p>
          Severity comes from the overall PSI: <b>critical</b> at ≥ 0.25, <b>warning</b> at ≥ 0.1. Webhooks are retried on
          rate limits and server errors (not on a bad URL), and every attempt, delivered or failed, is recorded below.
        </p>
      </Explainer>
      <ErrorNote error={alerts.error} what="the alert service" />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatTile label="Alerts recorded" value={alerts.data ? list.length : "—"} hint="latest 200" />
        <StatTile label="Delivered" value={alerts.data ? sent : "—"} />
        <StatTile label="Failed" value={alerts.data ? list.length - sent : "—"} />
        <StatTile label="Last alert" value={list[0] ? ago(list[0].created_at, now) : "never"} />
      </div>

      <div className="mt-6 grid gap-6 lg:grid-cols-3">
        <Card title="Channels" subtitle="Configured on the Go server via environment variables" pad={false}>
          <ul className="divide-y divide-line">
            {CHANNELS.map((c) => {
              const on = configured.has(c.id);
              return (
                <li key={c.id} className="flex items-center gap-3 px-5 py-3.5">
                  <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-surface-2 text-ink-2">
                    <c.Icon size={17} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium text-ink">{c.label}</div>
                    <div className="truncate text-xs text-ink-3">{on ? c.how : <>set <code className="font-mono">{c.env}</code></>}</div>
                  </div>
                  <span className={cx("inline-flex items-center gap-1 text-xs font-medium", on ? "text-good-ink" : "text-ink-3")}>
                    {on ? <CheckCircle2 size={13} /> : <BellOff size={13} />}
                    {on ? "Active" : "Not set"}
                  </span>
                </li>
              );
            })}
          </ul>
          <div className="border-t border-line px-5 py-4">
            <Button onClick={sendTest} loading={sending} disabled={channels.data !== undefined && configured.size === 0} className="w-full">
              <Send size={14} /> Send a test alert
            </Button>
            {channels.data && configured.size === 0 && (
              <p className="mt-2 text-xs leading-relaxed text-ink-3">
                Add a webhook URL to <code className="font-mono">.env</code> and restart the server (
                <code className="font-mono">docker compose up -d server</code>) to enable a channel.
              </p>
            )}
            {result && (
              <p className={cx("mt-2 flex items-start gap-1.5 text-xs", result.ok ? "text-good-ink" : "text-critical")}>
                {result.ok ? <CheckCircle2 size={13} className="mt-px shrink-0" /> : <XCircle size={13} className="mt-px shrink-0" />}
                {result.text}
              </p>
            )}
          </div>
        </Card>

        <Card title="History" subtitle="Newest first" className="lg:col-span-2" pad={false}>
          {list.length === 0 ? (
            <EmptyState icon={<BellOff size={28} />} title={alerts.data ? "No alerts yet" : "Loading…"}>
              {alerts.data && "That's good news: nothing has drifted. Send a test alert to check your webhooks."}
            </EmptyState>
          ) : (
            <ul className="max-h-[560px] divide-y divide-line overflow-auto">
              {list.map((a) => {
                const s = SEVERITY[a.severity];
                return (
                  <li key={a.id} className="flex gap-3 px-5 py-3">
                    <s.Icon size={17} className={cx("mt-0.5 shrink-0", s.cls)} aria-label={s.label} />
                    <div className="min-w-0 flex-1">
                      <div className="text-sm text-ink">{a.message}</div>
                      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-ink-3">
                        <span className={s.cls}>{s.label}</span>
                        <span className="capitalize">{a.channel}</span>
                        <span className={cx("inline-flex items-center gap-1", a.status === "sent" ? "text-good-ink" : "text-critical")}>
                          {a.status === "sent" ? <CheckCircle2 size={11} /> : <XCircle size={11} />}
                          {a.status}
                        </span>
                        <span title={dateTime(a.created_at)}>{ago(a.created_at, now)}</span>
                      </div>
                      {a.error && <div className="mt-1 truncate font-mono text-[11px] text-critical">{a.error}</div>}
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>
      </div>
    </>
  );
}
