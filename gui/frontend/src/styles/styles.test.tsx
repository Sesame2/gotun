import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FormControlLabel, Switch } from '@mui/material';
import { afterEach, beforeEach, expect, it } from 'vitest';
import globalStyles from './global.css?raw';
import layoutStyles from './layout.css?raw';

let styles: HTMLStyleElement;
beforeEach(() => {
  styles = document.createElement('style');
  styles.textContent = globalStyles + layoutStyles;
  document.head.appendChild(styles);
});
afterEach(() => { styles.remove(); });

it('keeps the real switch label clickable under the application stylesheet', async () => {
  const labelRule = Array.from(styles.sheet!.cssRules).find(rule => rule.cssText.startsWith('.MuiFormControlLabel-root {'));
  expect(labelRule?.cssText).not.toContain('pointer-events: none');
  render(<FormControlLabel control={<Switch />} label="Enable proxy" />);
  expect(getComputedStyle(screen.getByText('Enable proxy').closest('label')!).pointerEvents).not.toBe('none');
  await userEvent.click(screen.getByText('Enable proxy'));
  expect(screen.getByRole('switch', { name: 'Enable proxy' })).toBeChecked();
});

it('aligns compact switch rows with form fields and leaves space before the label', () => {
  // jsdom's CSS cascade does not reliably resolve MUI specificity against the global margin reset.
  const labelRule = Array.from(styles.sheet!.cssRules).find(rule => rule.cssText.startsWith('.MuiFormControlLabel-root {')) as CSSStyleRule;
  expect(labelRule.style.getPropertyValue('margin-left')).toBe('0');
  expect(labelRule.style.getPropertyPriority('margin-left')).toBe('important');
  render(<FormControlLabel control={<Switch />} label="Enable proxy" />);
  const label = screen.getByText('Enable proxy').closest('label')!;
  const labelStyle = getComputedStyle(label);
  expect(labelStyle.marginLeft).toBe('0px');
  expect(labelStyle.marginRight).toBe('0px');
  expect(labelStyle.columnGap).toBe('8px');
});

it('allows flex content to shrink and long detail text to wrap', () => {
  const { container } = render(<div className="layout"><main className="layout-content"><div className="main">{'long-host-name'.repeat(50)}</div></main></div>);
  const content = container.querySelector('.layout-content')!;
  const main = container.querySelector('.main')!;
  expect(getComputedStyle(content).minWidth).toBe('0');
  expect(getComputedStyle(main).minHeight).toBe('0');
  expect(getComputedStyle(main).overflowWrap).toBe('anywhere');
});

it('includes visible keyboard focus and a reduced-motion stylesheet rule', () => {
  const rules = Array.from(styles.sheet!.cssRules);
  expect(rules.some(rule => rule.cssText.includes(':focus-visible') && rule.cssText.includes('outline'))).toBe(true);
  expect(rules.some(rule => rule.cssText.includes('prefers-reduced-motion: reduce') && rule.cssText.includes('transition-duration'))).toBe(true);
});
