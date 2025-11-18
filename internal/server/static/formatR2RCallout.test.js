// Simple test for formatR2RCallout function
// Run with: node formatR2RCallout.test.js

const { formatR2RCallout } = require('./formatR2RCallout.js');
const assert = require('assert');

function test(description, fn) {
    try {
        fn();
        console.log('✓', description);
    } catch (err) {
        console.error('✗', description);
        console.error('  ', err.message);
        process.exit(1);
    }
}

// Test 1: Should return empty string for fast PRs (≤1.5 hours by default)
test('Returns empty for PRs with avgOpenHours <= 1.5 (default)', () => {
    const result = formatR2RCallout(0.5, 50000, 60, 70);
    assert.strictEqual(result, '');
});

test('Returns empty for PRs with avgOpenHours = 1.5 (default)', () => {
    const result = formatR2RCallout(1.5, 50000, 60, 70);
    assert.strictEqual(result, '');
});

// Test 2: Should render callout for slow PRs (>1.5 hours by default)
test('Renders callout for PRs with avgOpenHours > 1.5 (default)', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.length > 0, 'Should return non-empty HTML');
});

// Test 3: Should contain "Pro-Tip:" text
test('Contains "Pro-Tip:" text', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.includes('Pro-Tip:'), 'Should contain "Pro-Tip:"');
});

// Test 4: Should contain "Ready-to-Review" link
test('Contains "Ready-to-Review" link', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.includes('Ready-to-Review'), 'Should contain "Ready-to-Review"');
    assert(result.includes('href="https://codegroove.dev/"'), 'Should link to codegroove.dev');
});

// Test 5: Should contain OSS pricing message
test('Contains OSS pricing message', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.includes('Free for OSS, cheap for everyone else'), 'Should contain OSS pricing message');
});

// Test 6: Should format savings in thousands (K)
test('Formats savings with K suffix for thousands', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.includes('$50K/yr'), 'Should format $50,000 as $50K/yr');
});

// Test 7: Should format savings in millions (M)
test('Formats savings with M suffix for millions', () => {
    const result = formatR2RCallout(10, 2500000, 60, 70);
    assert(result.includes('$2.5M/yr'), 'Should format $2,500,000 as $2.5M/yr');
});

// Test 8: Should format small savings without suffix
test('Formats small savings without suffix', () => {
    const result = formatR2RCallout(10, 500, 60, 70);
    assert(result.includes('$500/yr'), 'Should format $500 as $500/yr');
});

// Test 9: Should contain "reducing merge times to <1.5h" (default)
test('Contains merge time reduction message (default 1.5h)', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.includes('reducing merge times to &lt;1.5h'), 'Should mention reducing merge times to <1.5h');
});

// Test 9b: Should use custom target merge time when provided
test('Uses custom target merge time when provided', () => {
    const result = formatR2RCallout(10, 50000, 60, 70, 2.0);
    assert(result.includes('reducing merge times to &lt;2.0h'), 'Should mention reducing merge times to <2.0h');
});

// Test 10: Should contain proper HTML structure
test('Contains proper HTML div wrapper', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.startsWith('<div'), 'Should start with <div');
    assert(result.endsWith('</div>'), 'Should end with </div>');
});

// Test 11: Should use green color scheme
test('Uses green color scheme', () => {
    const result = formatR2RCallout(10, 50000, 60, 70);
    assert(result.includes('#00c853'), 'Should include green color #00c853');
});

console.log('\nAll tests passed! ✓');
