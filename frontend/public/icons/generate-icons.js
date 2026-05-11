<script>
// Simple PWA icon generator - run with Node.js to create PNG icons
// Requires: npm install sharp
// Usage: node generate-icons.js
const sharp = require('sharp');

const iconSvg = `
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">
  <rect width="512" height="512" fill="#0d6efd" rx="64"/>
  <path d="M128 128 L384 384 M384 128 L128 384" stroke="white" stroke-width="64" stroke-linecap="round"/>
  <text x="256" y="420" text-anchor="middle" fill="white" font-size="80" font-family="Arial, sans-serif" font-weight="bold">X</text>
</svg>
`;

// Simplified - use a canvas-based approach
const sizes = [72, 96, 128, 144, 152, 192, 384, 512];
console.log('Icon generator requires sharp package: npm install sharp');
console.log('Or use online generator: https://favicon.io/icon-generator');
sizes.forEach(size => {
  console.log(`Need icon: icons/icon-${size}x${size}.png`);
});
