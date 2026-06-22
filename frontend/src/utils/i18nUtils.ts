import { useTranslation } from 'react-i18next'

// The Console plugin i18n namespace follows the convention: plugin__<pluginName>.
// All user-facing strings are keyed under this namespace in src/locales/en/plugin__ossm-acm.json.
// The Console loads the merged locale bundle at runtime; react-i18next is provided
// by the Console host, so this plugin never initializes i18next itself.
export const PLUGIN_I18N_NAMESPACE = 'plugin__ossm-acm'

export const useMeshTranslation = () => useTranslation(PLUGIN_I18N_NAMESPACE)
