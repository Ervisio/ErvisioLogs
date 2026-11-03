/** Entry of the Ervisio Logs plugin (SDK v3): stores the SDK and React, then registers the strings, styles and page. */
import { setReact } from '@ervisio/plugin-sdk/react';
import { setSdk, type PluginSDK } from './sdk';
import { strings } from './strings';
import { injectStyles } from './styles';
import { LogsPage } from './views/LogsPage';

export default function activate(sdk: PluginSDK): void {
  setSdk(sdk);
  setReact(sdk.react);
  sdk.registerStrings(strings);
  injectStyles();
  sdk.registerPage('logs', LogsPage);
}
