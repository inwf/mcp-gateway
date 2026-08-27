import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { DeleteOutlined } from '@ant-design/icons';
import { renderWithProviders } from '@/test/harness';
import { IconButton } from './IconButton';

// antd labels every icon with its own English identifier, and that
// label becomes the button's accessible name unless something else
// supplies one. Left alone, the delete button on every row announces
// itself as "delete" — in English, whatever the interface language.
describe('a button that is only an icon', () => {
  it('is named by its label rather than by its icon', () => {
    renderWithProviders(<IconButton label="删除" icon={<DeleteOutlined />} />);

    expect(screen.getByRole('button', { name: '删除' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
  });

  it('passes through what a button takes', () => {
    renderWithProviders(<IconButton label="删除" icon={<DeleteOutlined />} disabled danger />);

    expect(screen.getByRole('button', { name: '删除' })).toBeDisabled();
  });
});
