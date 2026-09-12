export interface AppFailure {
    code: string;
    message: string;
    retryAfter?: number;
    httpStatus?: number;
    upstreamCode?: number;
}
export interface Comment {
    id: string;
    author: { id: string; nickname: string; avatar?: string };
    text: string;
    createdAt: string;
    replyCount?: number;
    primaryCommentId?: string;
    replyTo?: { id: string; nickname: string; summary: string };
}
export interface CommentPage {
    items: Comment[];
    cursor: string;
    complete: boolean;
}
export interface CommentCreated { comment: Comment }
export type CommentOrder = 'hot' | 'latest';
export interface Item {
    kind: 'episode' | 'podcast';
    id: string;
    title: string;
    podcastId?: string;
    podcastTitle?: string;
    description?: string;
    showNotes?: string;
    image?: string;
    duration?: number;
    published?: string;
    sourceUrl: string;
    restricted: boolean;
    restriction?: string;
}
export interface Session {
    epoch: number;
    state: string;
    identity?: {
        id: string;
        nickname: string;
        avatar?: string;
    };
    persistent: boolean;
    storageAvailable: boolean;
}
export interface Settings {
    volume: number;
    rate: number;
    closeBehavior: 'ask' | 'tray' | 'exit';
    experimentalAccount: boolean;
}
export interface Progress {
    item: Item;
    position: number;
    duration: number;
    ended: boolean;
    updatedAt: string;
}
export interface Library {
    items: Item[];
    cursor: string;
    status: string;
    complete: boolean;
    updatedAt?: string;
    error?: AppFailure;
    pages: number;
    revision: number;
    epoch: number;
}
export interface Bootstrap {
    version: string;
    adapter: string;
    session: Session;
    settings: Settings;
    queue: Item[];
    bookmarks: Item[];
    history: Progress[];
    playbackGeneration: number;
    discoveryGeneration?: number;
    warning?: AppFailure;
}
export interface Playback {
    item: Item;
    url: string;
    epoch: number;
    position: number;
}
export interface DesktopInfo {
    tray: boolean;
    mediaKey: boolean;
    demo: boolean;
    message?: string;
}
export type Call = <T = unknown>(action: string, payload?: unknown) => Promise<T>;

export type SearchKind = 'podcast' | 'episode' | 'user';
export interface Creator { id: string; nickname: string; avatar?: string; bio?: string }
export type SubscriptionState = 'subscribed' | 'not_subscribed' | 'unknown';
export interface SearchPage { items: Item[]; users: Creator[]; subscriptions: Record<string, SubscriptionState>; cursor: string; complete: boolean }
export interface CreatorPage { creator: Creator; items: Item[]; subscriptions: Record<string, SubscriptionState>; cursor: string; complete: boolean }
export interface SubscriptionResult { podcastId: string; state: SubscriptionState; item?: Item; warning?: AppFailure }
