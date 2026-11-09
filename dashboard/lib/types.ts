export type Site = { id: string; name: string; domain: string; createdAt: number; pageviews?: number; lastEventAt?: number };

export type KPIs = {
  clicks: number;
  pageviews: number;
  uniqueVisitors: number;
  pages: number;
  rageClicks: number;
  deadClicks: number;
  navigations: number;
  avgScrollDepth: number;
  avgTimeOnPageMs: number;
  entries: number;
  exits: number;
  bounces: number;
  bounceRate: number;
  exitRate: number;
  pagesPerSession: number;
  clicksPerView: number;
};

export type Day = { date: string; clicks: number; pageviews: number; visitors: number };
export type TimeOfDay = { hours: number[]; days: number[]; matrix: number[][]; total: number; peak: { hour: number; day: number } };
export type Reach = { depth: number; pageviews: number; share: number };
export type ScrollStats = { pageviews: number; avgDepth: number; avgTimeMs: number; reach: Reach[]; buckets: number[] };
export type DeviceStat = { device: string; pageviews: number; visitors: number; clicks: number; share: number };

export type PageRow = {
  url: string;
  path: string;
  clicks: number;
  uniqueVisitors: number;
  pageviews: number;
  avgScrollDepth: number;
  avgTimeMs: number;
  entries: number;
  exits: number;
  exitRate: number;
  bounceRate: number;
  rageClicks: number;
  deadClicks: number;
  firstSeen: number;
  lastSeen: number;
  hasScreenshot: boolean;
};
export type PageList = { total: number; limit: number; offset: number; sort: string; order: string; rows: PageRow[] };

export type ElementStat = {
  pageUrl: string;
  selector: string;
  label: string;
  tag: string;
  id: string;
  classes: string;
  text: string;
  ariaLabel: string;
  clicks: number;
  visitors: number;
  rageClicks: number;
  deadClicks: number;
  share: number;
  relX: number;
  relY: number;
};
export type ElementList = { totalClicks: number; rows: ElementStat[] };

export type PageCount = { url: string; count: number; share: number };
export type ExitPoint = { pageUrl: string; selector: string; label: string; tag: string; count: number; share: number };
export type EntryExit = { entries: PageCount[]; exits: PageCount[]; exitPoints: ExitPoint[] };
export type Referrer = { source: string; count: number; share: number };
export type FlowEdge = { url: string; kind: string; external: boolean; count: number; share: number };
export type Flow = { pageviews: number; inbound: FlowEdge[]; outbound: FlowEdge[]; unloads: number; exitPoints: ExitPoint[] };

export type Heatmap = {
  cols: number;
  rows: number;
  total: number;
  max: number;
  visitors: number;
  docW: number;
  docH: number;
  viewportW: number;
  source: string;
  cells: { x: number; y: number; n: number }[];
};

export type CompareSite = {
  site: Site;
  kpis: KPIs;
  daily: Day[];
  hours: number[];
  days: number[];
  reach: Reach[];
  devices: DeviceStat[];
  topPages: PageRow[];
};

export type Component = {
  id: string;
  siteId: string;
  name: string;
  selector: string;
  attrName: string;
  attrValue: string;
  pagePattern: string;
  createdAt: number;
  updatedAt: number;
  browserSelector: string;
  evaluable: boolean;
  clicks: number;
  visitors: number;
  lastSeen: number;
};

export type ComponentList = { components: Component[]; limit: number };

export type ComponentStats = {
  clicks: number;
  visitors: number;
  rageClicks: number;
  deadClicks: number;
  siteClicks: number;
  clickShare: number;
  exposedPageviews: number;
  clickedPageviews: number;
  clickRate: number;
  firstSeen: number;
  lastSeen: number;
  pages: PageCount[];
  exposureSource: 'pattern' | 'clicked-pages';
};

export type MatchPreview = {
  matchedClicks: number;
  totalClicks: number;
  share: number;
  pages: PageCount[];
  elements: { pageUrl: string; selector: string; label: string; clicks: number }[];
  evaluable: boolean;
};

export type DiscoveredAttrs = { attribute: string; values: { value: string; clicks: number; pages: number; componentId?: string }[] };
