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

  const servers = useMemo(
    () => [...new Set((tools.data ?? []).map((tool) => tool.server))].sort(),
    [tools.data],
  );

  const shown = useMemo(
    () => (tools.data ?? []).filter((tool) => !server || tool.server === server),
    [tools.data, server],
  );

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

      <Panel title={t('tools.title')} count={shown.length}>
        {tools.isPending ? (
          <Skeleton active paragraph={{ rows: 5 }} title={false} />
        ) : tools.isError ? (
          <ErrorNotice error={tools.error} onRetry={() => void tools.refetch()} />
        ) : (tools.data ?? []).length === 0 ? (
          <Nothing title={t('tools.empty')} hint={t('tools.emptyHint')} />
        ) : shown.length === 0 ? (
          <Nothing title={t('tools.noMatch')} />
        ) : (
          <div className={styles.grid}>
            {shown.map((tool, index) => {
              // The gateway prefixes and, on a collision, renames. What
              // a client must call is `exposed`, which is not always
              // derivable from the upstream name.
              const renamed = !tool.exposed.endsWith(tool.tool);

              return (
                <Reveal key={tool.exposed} index={index}>
                  <div className={styles.card}>
                    <div className={styles.head}>
                      <span className={styles.exposed}>{tool.exposed}</span>
                      <span className={styles.server}>{tool.server}</span>
                    </div>

                    {tool.description ? (
                      <p className={styles.description}>{tool.description}</p>
                    ) : null}

                    <div className={styles.foot}>
                      <Tooltip title={`${t('tools.from')} ${tool.server} · ${tool.tool}`}>
                        <span className={cx(styles.origin, renamed && styles.renamed)}>
                          {tool.tool}
                        </span>
                      </Tooltip>
                      <Button
                        size="small"
                        icon={<ThunderboltOutlined aria-hidden />}
                        onClick={() => setCalling(tool)}
                      >
                        {t('tools.call')}
                      </Button>
                    </div>
                  </div>
                </Reveal>
              );
            })}
          </div>
        )}
      </Panel>

      <ToolCallDialog
        open={calling !== null}
        onClose={() => setCalling(null)}
        server={calling?.server ?? ''}
        tool={calling ? asTool(calling) : null}
      />
    </div>
  );
}
