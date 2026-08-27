import { Button, Tooltip } from 'antd';
import type { ButtonProps } from 'antd';
import type { ReactNode } from 'react';

/**
 * A button that is only an icon.
 *
 * The label is used twice, which is the point: as the tooltip a sighted
 * user hovers for, and as the accessible name a screen reader reads.
 * Those are the same fact and should not be stated separately.
 *
 * Without the explicit name, the name would come from the icon, which
 * antd labels with its own identifier — so the delete button announces
 * itself as "delete" rather than as whatever the tooltip says, in
 * English, regardless of the interface language.
 */
export function IconButton({
  label,
  icon,
  placement = 'top',
  ...rest
}: {
  label: string;
  icon: ReactNode;
  placement?: 'top' | 'right' | 'bottom' | 'left' | undefined;
} & Omit<ButtonProps, 'icon' | 'aria-label' | 'children'>) {
  return (
    <Tooltip title={label} placement={placement}>
      <Button type="text" icon={icon} aria-label={label} {...rest} />
    </Tooltip>
  );
}
