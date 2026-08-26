export function PageLoading() { return <div className="page-state"><span className="spinner" />Loading usage data…</div> }
export function PageError({ message, retry }: { message: string; retry: () => void }) { return <div className="page-state error"><strong>Couldn’t load this view</strong><span>{message}</span><button className="button" onClick={retry}>Try again</button></div> }
