/** Returns the default landing page URL for the Fleet Service Mesh perspective. */
export const landingPageURL = (
  _flags: Record<string, boolean>,
  _isFirstVisit: boolean,
): string => '/fleet-mesh/overview'

/** Returns the redirect URL when importing resources within this perspective. */
export const importRedirectURL = (_namespace: string): string => '/fleet-mesh/overview'
