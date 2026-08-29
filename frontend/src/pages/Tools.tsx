import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Button, Input, Select, Skeleton, Tooltip } from 'antd';
import { ThunderboltOutlined } from '@ant-design/icons';
import { endpoints } from '@/api/endpoints';
import { keys } from '@/api/query';
import type { AggregatedTool, Tool } from '@/api/types';
import { Panel } from '@/components/Panel';
import { ErrorNotice } from '@/components/ErrorNotice';
import { Nothing } from '@/components/Nothing';
import { Reveal } from '@/components/Reveal';
import { ToolCallDialog } from '@/components/ToolCallDialog';
import { cx } from '@/lib/cx';
import styles from './Tools.module.css';

/**
 * The gateway offers two kinds of tool, and they are shown apart.
 *
 * Its own tools — list_servers and the rest — are how a model finds its
 * way around an installation; they belong to no server and are called
 * through a route of their own. The forwarded ones each come from a
 * server, under a name the gateway assigns. Mixing them into one list
 * would bury the seven that explain the rest.
 */

/** Turns an aggregated tool into what the call dialog takes. The dialog
 *  works in terms of one server's tool, since that is what the call
 *  endpoint addresses. */
function asTool(aggregated: AggregatedTool): Tool {
  return {
    name: aggregated.tool,
    ...(aggregated.description !== undefined ? { description: aggregated.description } : {}),
    ...(aggregated.inputSchema !== undefined ? { inputSchema: aggregated.inputSchema } : {}),
  };
}

/**
 * Filters the gateway's own tools by the search text.
 *
 * The forwarded tools are searched by the gateway, which scores and
 * orders them. That endpoint aggregates upstream servers and so does not
 * see these, which leaves the choice between filtering them here and
 * letting a search quietly apply to only half of what is on offer. Every
 * term has to match, which is the rule the gateway's own search uses.
 */
function matching(tools: AggregatedTool[], search: string): AggregatedTool[] {
  const terms = search.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return tools;

  return tools.filter((tool) => {
    const haystack = `${tool.exposed} ${tool.description ?? ''}`.toLowerCase();
    return terms.every((term) => haystack.includes(term));
  });
}

function ToolCard({
  tool,
  index,
  onCall,
}: {
  tool: AggregatedTool;
  index: number;
  onCall: () => void;
}) {
  const { t } = useTranslation();

  // The gateway prefixes and, on a collision, renames. What a client must
  // call is `exposed`, which is not always derivable from the upstream
  // name. A gateway tool has no server and is never renamed.
  const renamed = tool.server !== '' && !tool.exposed.endsWith(tool.tool);

  return (
    <Reveal index={index}>
      <div className={styles.card}>
        <div className={styles.head}>
          <span className={styles.exposed}>{tool.exposed}</span>
          {tool.server ? <span className={styles.server}>{tool.server}</span> : null}
        </div>

        {tool.description ? <p className={styles.description}>{tool.description}</p> : null}

        <div className={styles.foot}>
          {tool.server ? (
            <Tooltip title={`${t('tools.from')} ${tool.server} · ${tool.tool}`}>
              <span className={cx(styles.origin, renamed && styles.renamed)}>{tool.tool}</span>
            </Tooltip>
          ) : (
            <span className={styles.origin}>{t('tools.builtIn')}</span>
          )}
          <Button size="small" icon={<ThunderboltOutlined aria-hidden />} onClick={onCall}>
            {t('tools.call')}
          </Button>
        </div>
      </div>
    </Reveal>
  );
}

export default function Tools() {
  const { t } = useTranslation();

  const [search, setSearch] = useState('');
  const [server, setServer] = useState('');
  const [calling, setCalling] = useState<AggregatedTool | null>(null);

  // The search runs on the gateway rather than here: it scores matches
  // across every connected server, and the result order is that score.
  const tools = useQuery({
    queryKey: keys.tools.aggregated(search, []),
    queryFn: () => endpoints.tools({ ...(search ? { search } : {}), limit: 200 }),
  });

  // The gateway's own tools come from the endpoint that reports what its
  // MCP server publishes, because they are on no upstream server and the
  // aggregated endpoint therefore cannot see them.
  const gateway = useQuery({
    queryKey: keys.gateway.tools(),
    queryFn: () => endpoints.gatewayTools(),
  });

  const system = useMemo<AggregatedTool[]>(() => {
    if (!gateway.data) return [];
    const own = new Set(gateway.data.systemTools);
    const listed = gateway.data.tools
      .filter((tool) => own.has(tool.name))
      .map((tool) => ({
        server: '',
        tool: tool.name,
        exposed: tool.name,
        ...(tool.description !== undefined ? { description: tool.description } : {}),
        ...(tool.inputSchema !== undefined ? { inputSchema: tool.inputSchema } : {}),
      }));
    return matching(listed, search);
  }, [gateway.data, search]);

  const servers = useMemo(
    () => [...new Set((tools.data ?? []).map((tool) => tool.server))].sort(),
    [tools.data],
  );

  const forwarded = useMemo(
    () => (tools.data ?? []).filter((tool) => !server || tool.server === server),
    [tools.data, server],
  );

  // Picking a server asks about that server. The gateway's own tools are
  // on no server, so they are not an answer to it.
  const shownSystem = server === '' ? system : [];

  const pending = tools.isPending || gateway.isPending;
  const failure = tools.error ?? gateway.error;
  const nothingAtAll = (tools.data ?? []).length === 0 && system.length === 0;

  if (failure) {
    return (
      <div className={styles.page}>
        <ErrorNotice
          error={failure}
          onRetry={() => {
            void tools.refetch();
            void gateway.refetch();
          }}
        />
      </div>
    );
  }

  return (
    <div className={styles.page}>
      <div className={styles.bar}>
        <Input.Search
          allowClear
          placeholder={t('tools.search')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ maxWidth: 320 }}
        />
        <Select
          value={server}
          onChange={setServer}
          style={{ minWidth: 170 }}
          options={[
            { label: `${t('logs.server')}: ${t('logs.all')}`, value: '' },
            ...servers.map((value) => ({ label: value, value })),
          ]}
        />
      </div>

      {pending ? (
        <Panel title={t('tools.title')}>
          <Skeleton active paragraph={{ rows: 5 }} title={false} />
        </Panel>
      ) : nothingAtAll ? (
        <Panel title={t('tools.title')}>
          <Nothing title={t('tools.empty')} hint={t('tools.emptyHint')} />
        </Panel>
      ) : (
        <>
          {shownSystem.length > 0 ? (
            <Panel title={t('tools.system')} count={shownSystem.length}>
              <div className={styles.grid}>
                {shownSystem.map((tool, index) => (
                  <ToolCard
                    key={tool.exposed}
                    tool={tool}
                    index={index}
                    onCall={() => setCalling(tool)}
                  />
                ))}
              </div>
            </Panel>
          ) : null}

          <Panel title={t('tools.fromServers')} count={forwarded.length}>
            {forwarded.length === 0 ? (
              <Nothing title={t('tools.noMatch')} />
            ) : (
              <div className={styles.grid}>
                {forwarded.map((tool, index) => (
                  <ToolCard
                    key={tool.exposed}
                    tool={tool}
                    index={index}
                    onCall={() => setCalling(tool)}
                  />
                ))}
              </div>
            )}
          </Panel>
        </>
      )}

      <ToolCallDialog
        open={calling !== null}
        onClose={() => setCalling(null)}
        server={calling?.server ?? ''}
        tool={calling ? asTool(calling) : null}
      />
    </div>
  );
}
