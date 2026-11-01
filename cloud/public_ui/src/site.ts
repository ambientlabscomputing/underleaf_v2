// Central place for outbound links and shared strings. Update these as the
// real destinations (docs, blog, sign-up) come online.
export const SITE = {
	name: 'Underleaf',
	description:
		'Deploy from a git repo onto your own hardware. Underleaf runs locally; the cloud only steps in when you want to reach your apps from outside.',
	accountUrl: 'https://underleafapp.com',
	docsUrl: 'https://underleafapp.com/docs',
	blogUrl: 'https://underleafapp.com/blog',
	deployCommand: 'ufctl deploy gh:you/your-app',
} as const;
