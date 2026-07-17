package decruft

var entryPointSelectors = []string{
	"#post",
	".post-content",
	".article-content",
	"#article-content",
	".article_post",
	".article-wrapper",
	".entry-content",
	".content-article",
	".instapaper_body",
	".post",
	".markdown-body",
	"article",
	`[role="article"]`,
	"main",
	`[role="main"]`,
	"body",
}

var blockElements = []string{
	"div", "section", "article", "main", "aside", "header", "footer", "nav", "content",
}

// testAttributes are the HTML attributes checked for partial-selector matches.
var testAttributes = []string{
	"class", "id", "data-test", "data-testid", "data-test-id", "data-qa", "data-cy",
}

// testAttributeSelectors are CSS selectors for elements that have any testAttribute.
var testAttributeSelectors = []string{
	"[class]", "[id]", "[data-test]", "[data-testid]", "[data-test-id]", "[data-qa]", "[data-cy]",
}

// exactSelectors are removed wholesale before extraction.
// Selectors with case-insensitive attribute flags (`i`) are omitted here;
// they are caught by the partial-selector regex pass instead.
var exactSelectors = []string{
	// scripts, styles
	"noscript",
	`script:not([type^="math/"])`,
	"style",
	"meta",
	"link",

	// ads
	`.ad:not([class*="gradient"])`,
	`[class^="ad-"]`,
	`[class$="-ad"]`,
	`[id^="ad-"]`,
	`[id$="-ad"]`,
	`[role="banner"]`,
	`[alt*="advert"]`,
	".promo",
	".Promo",
	"#barrier-page",
	".alert",

	// comments
	`[id="comments"]`,
	`[id="comment"]`,

	// header, nav
	"header",
	`.header:not(.banner)`,
	"#header",
	"#Header",
	"#banner",
	"#Banner",
	"nav",
	".navigation",
	"#navigation",
	".hero",
	`[role="navigation"]`,
	`[role="dialog"]`,
	`[role*="complementary"]`,
	`[class*="pagination"]`,
	".menu",
	"#menu",
	"#siteSub",
	".previous",

	// metadata
	".author",
	".Author",
	`[class$="_bio"]`,
	"#categories",
	".contributor",
	".date",
	"#date",
	"[data-date]",
	".entry-meta",
	".meta",
	".tags",
	"#tags",
	".toc",
	".Toc",
	"#toc",
	".headline",
	"#headline",
	"#title",
	"#Title",
	"#articleTag",
	`[href*="/category"]`,
	`[href*="/categories"]`,
	`[href*="/tag/"]`,
	`[href*="/tags/"]`,
	`[href*="/topics"]`,
	`[href*="author"]`,
	`[href*="#toc"]`,
	`[href="#top"]`,
	`[href="#Top"]`,
	`[href="#page-header"]`,
	`[href="#content"]`,
	`[href="#site-content"]`,
	`[href="#main-content"]`,
	`[href^="#main"]`,
	`[src*="author"]`,

	// footer
	"footer",

	// inputs, forms, interactive
	".aside",
	`aside:not([class*="callout"])`,
	"button",
	"canvas",
	"date",
	"dialog",
	"fieldset",
	"form",
	`input:not([type="checkbox"])`,
	"label",
	"option",
	"select",
	"textarea",
	"time",
	"relative-time",

	// hidden
	"[hidden]",
	`[aria-hidden="true"]:not([class*="math"])`,
	`[style*="display: none"]:not([class*="math"])`,
	`[style*="display:none"]:not([class*="math"])`,
	`[style*="visibility: hidden"]`,
	`[style*="visibility:hidden"]`,
	`[style*="opacity:0"]`,
	`[style*="opacity: 0"]`,
	".hidden",
	".invisible",

	// iframes (keep video embeds)
	`iframe:not([src*="youtube"]):not([src*="youtu.be"]):not([src*="vimeo"]):not([src*="twitter"]):not([src*="x.com"]):not([src*="datawrapper"])`,

	// logos
	`[class="logo"]`,
	"#logo",
	"#Logo",

	// newsletter
	"#newsletter",
	"#Newsletter",
	".subscribe",

	// print-only / no-print
	".noprint",
	`[data-print-layout="hide"]`,
	`[data-block="donotprint"]`,

	// link lists / sidebar
	`[data-container*="most-viewed"]`,
	".sidebar",
	".Sidebar",
	"#sidebar",
	"#Sidebar",
	"#sitesub",

	// skip links
	`[aria-label*="skip"]`,

	// misc
	".copyright",
	"#copyright",
	"#rss",
	"#feed",
	".gutter",
	"#primaryaudio",
	"table.infobox",
}

// partialSelectors are matched case-insensitively against testAttributes.
var partialSelectors = []string{
	"a-statement", "access-wall", "activitypub", "actioncall", "addcomment",
	"advert", "adlayout", "ad-tldr", "ad-placement", "ads-container", "_ad_",
	"after_content", "after_main_article", "afterpost", "allterms", "-alert-",
	"alert-box", "appendix", "_archive", "around-the-web", "aroundpages",
	"article-author", "article-badges", "article-banner", "article-bottom-section",
	"article-bottom", "article-category", "article-card", "article-citation",
	"article__copy", "article_date", "article-date", "article-end ",
	"article_header", "article-header", "article__header", "article__hero",
	"article__info", "article-info", "article-meta", "article_meta",
	"article__meta", "articlename", "article-subject", "article_subject",
	"article-snippet", "article-separator", "article--share", "article--topics",
	"articletags", "article-tags", "article_tags", "articletitle",
	"article-title", "article_title", "articletopics", "article-topics",
	"article--lede", "articlewell", "associated-people", "audio-card",
	"author-bio", "author-box", "author-info", "author_info", "authorm",
	"author-mini-bio", "author-name", "author-publish-info", "authored-by", "avatar",
	"back-to-top", "backlink_container", "backlinks-section",
	"bio-block", "biobox", "blog-pager", "bookmark-", "-bookmark",
	"bottominfo", "bottomnav", "bottom-of-article", "bottom-wrapper",
	"brand-bar", "breadcrumb", "brdcrumb", "button-wrapper", "buttons-container",
	"btn-", "-btn", "byline",
	"captcha", "card-text", "card-media", "card-post", "carouselcontainer",
	"carousel-container", "cat_header", "catlinks", "_categories",
	"card-author", "card-content", "chapter-list", "collections", "comments",
	"commentbox", "comment-button", "commentcomp", "comment-content",
	"comment-count", "comment-form", "comment-number", "comment-respond",
	"comment-thread", "comment-wrap", "complementary", "consent",
	"contact-", "content-card", "content-topics", "contentpromo",
	"context-bar", "context-widget", "core-collateral", "cover-",
	"created-date", "creative-commons_", "c-subscribe", "_cta", "-cta",
	"cta-", "cta_", "current-issue", "custom-list-number",
	"dateline", "dateheader", "date-header", "date-pub", "disclaimer",
	"disclosure", "discussion", "discuss_", "disqus", "donate", "donation",
	"dropdown",
	"eletters", "emailsignup", "engagement-widget", "enhancement",
	"entry-author-info", "entry-categories", "entry-date", "entry-title",
	"entry-utility", "-error", "error-", "eyebrow", "expand-reduce",
	"external-anchor", "externallinkembedwrapper", "extra-services", "extra-title",
	"facebook", "fancy-box", "favorite", "featured-content", "feature_feed",
	"feedback", "feed-links", "field-site-sections", "fixheader", "floating-vid",
	"follower", "footer", "footnote-back", "footnoteback", "form-group",
	"for-you", "frontmatter", "further-reading", "fullbleedheader",
	"gated-", "gh-feed", "gist-meta", "goog-", "graph-view",
	"hamburger", "header_logo", "header-logo", "header-pattern", "hero-list",
	"hide-for-print", "hide-print", "hide-when-no-script", "hidden-print",
	"hidden-sidenote", "hidden-accessibility",
	"infoline", "instacartIntegration", "interlude", "interaction",
	"itemendrow", "invisible",
	"jumplink", "jump-to-", "js-skip-to-content",
	"keepreading", "keep-reading", "keep_reading", "keyword_wrap", "kicker",
	"labstab", "-labels", "language-name", "lastupdated", "latest-content",
	"-ledes-", "-license", "license-", "lightbox-popup", "like-button",
	"link-box", "links-grid", "links-title", "listing-dynamic-terms",
	"list-tags", "listinks", "loading", "loa-info", "logo_container",
	"ltx_role_refnum", "ltx_tag_bibitem", "ltx_error",
	"masthead", "marketing", "media-inquiry", "-menu", "menu-",
	"metadata", "might-like", "minibio", "more-about", "_modal", "-modal",
	"more-", "morenews", "morestories", "more_wrapper", "most-read",
	"move-helper", "mw-editsection", "mw-cite-backlink", "mw-indicators",
	"mw-jump-link",
	"nav-", "nav_", "navigation-post", "next-", "newsgallery",
	"news-story-title", "newsletter_", "newsletterbanner",
	"newslettercontainer", "newsletter-form", "newsletter-signup",
	"newslettersignup", "newsletterwidget", "newsletterwrapper",
	"not-found", "notessection", "nomobile", "noprint",
	"open-slideshow", "originally-published", "other-blogs", "outline-view",
	"pagehead", "page-header", "page-title", "paywall_message", "-partners",
	"permission-", "plea", "popular", "popup_links", "pop_stories", "pop-up",
	"post-author", "post-bottom", "post__category", "postcomment",
	"postdate", "post-date", "post_date", "post-details", "post-feeds",
	"postinfo", "post-info", "post_info", "post-inline-date", "post-links",
	"postlist", "post_list", "post_meta", "post-meta", "postmeta",
	"post_more", "postnavi", "post-navigation", "postpath", "post-preview",
	"postsnippet", "post_snippet", "post-snippet", "post-subject",
	"posttax", "post-tax", "post_tax", "posttag", "post_tag", "post-tag",
	"post_time", "posttitle", "post-title", "post_title", "post__title",
	"post-ufi-button", "prev-post", "prevnext", "prev_next", "prev-next",
	"previousnext", "press-inquiries", "print-none", "print-header",
	"print:hidden", "privacy-notice", "privacy-settings", "profile",
	"promo_article", "promo-bar", "promo-box", "pubdate", "pub_date",
	"pub-date", "publish_date", "publish-date", "publication-date",
	"publicationName",
	"qr-code", "qr_code", "quick_up",
	"_rail", "ratingssection", "read_also", "readmore", "read-next",
	"read_next", "read_time", "read-time", "reading_time", "reading-time",
	"reading-list", "recent-", "recent-articles", "recentpost", "recent_post",
	"recent-post", "recommend", "redirectedfrom", "recirc", "register",
	"related", "relevant", "reversefootnote", "_rss", "rss-link",
	"screen-reader-text", "scroll_to", "scroll-to", "_search", "-search",
	"section-nav", "series-banner", "share-box", "sharedaddy",
	"share-icons", "sharelinks", "share-post", "share-print", "share-section",
	"show-for-print", "sidebartitle", "sidebar-content", "sidebar-wrapper",
	"sideitems", "sidebar-author", "sidebar-item", "side-box", "side-logo",
	"sign-in-gate", "similar-", "similar_", "similars-", "site-index",
	"site-header", "siteheader", "site-logo", "site-name", "site-wordpress",
	"skip-content", "skip-to-content", "skip-link", "c-skip-link",
	"_skip-link", "-slider", "slug-wrap", "social-author", "social-shar",
	"social-date", "speechify-ignore", "speedbump", "sponsor",
	"springercitation", "sr-only", "_stats", "story-date",
	"story-navigation", "storyreadtime", "storysmall", "storypublishdate",
	"subject-label", "subhead", "submenu", "-subscribe-",
	"subscriber-drive", "subscription-",
	"_tags", "tags__item", "tag_list", "taxonomy", "table-of-contents",
	"tabs-", "terminaltout", "time-rubric", "timestamp", "time-read",
	"time-to-read", "tip_off", "tiptout", "-tout-", "toc-container",
	"toggle-caption", "tooltip", "topbar", "topic-list", "topic-subnav",
	"top-wrapper", "tree-item", "trending", "trust-feat", "trust-badge",
	"trust-project", "twitter",
	"u-hide", "upsell",
	"viewbottom", "visually-hidden", "welcomebox", "widget_pages",
}

// contentIndicators in class/id suggest the element is likely content.
var contentIndicators = []string{
	"admonition", "article", "content", "entry", "image", "img",
	"font", "figure", "figcaption", "pre", "main", "post", "story", "table",
}

// navigationIndicators in text content suggest the element is navigation/chrome.
var navigationIndicators = []string{
	"advertisement", "all rights reserved", "banner", "cookie", "comments",
	"copyright", "follow me", "follow us", "footer", "header", "homepage",
	"login", "menu", "more articles", "more like this", "most read", "nav",
	"navigation", "newsletter", "popular", "privacy", "recommended", "register",
	"related", "responses", "share", "sidebar", "sign in", "sign up", "signup",
	"social", "sponsored", "subscribe", "terms", "trending",
}

// nonContentPatterns in class/id lower the score but don't force removal.
var nonContentPatterns = []string{
	"ad", "banner", "cookie", "copyright", "footer", "header", "homepage",
	"menu", "nav", "newsletter", "popular", "privacy", "recommended", "related",
	"rights", "share", "sidebar", "social", "sponsored", "subscribe", "terms",
	"trending", "widget",
}
