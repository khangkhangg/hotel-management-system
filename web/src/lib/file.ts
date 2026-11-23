/** The bytes of a chosen file as base64 text, which is how pictures are sent to the server. */
export function readBase64(file: File): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.onerror = () => reject(reader.error);
		reader.onload = () => resolve(String(reader.result).split(',')[1] ?? '');
		reader.readAsDataURL(file);
	});
}
