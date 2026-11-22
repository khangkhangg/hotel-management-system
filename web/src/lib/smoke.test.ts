import { expect, test } from 'vitest';

test('the test runner and jest-dom matchers work', () => {
	document.body.innerHTML = '<button>Close chat</button>';
	expect(document.querySelector('button')).toBeInTheDocument();
});
