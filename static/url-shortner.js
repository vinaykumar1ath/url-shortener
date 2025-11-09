document.getElementById('submitBtn').addEventListener('click', shortenurlAPI);

async function shortenurlAPI() {
    const inputText = document.getElementById('inputText').value;
    if (!inputText) {
        alert("Please enter some text.");
        return;
    }

    const data = { "url": inputText };
    try {
        const response = await fetch(`/`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(data),
        });

        const responseData = await response.json();
        const responseMessage = document.getElementById('responseMessage');
        const textHolder = document.getElementById('textHolder');
        const urlText = document.getElementById('urlText');
        const copyBtn = document.getElementById('copyBtn');

        if (response.ok) {
            // If 200 OK response
            responseMessage.textContent = `Message: ${responseData.message}`;
            responseMessage.className = 'response-message success';

            // Set the full URL (base address + sURL) as the text content
            const baseURL = window.location.origin; // Get the base URL (root of the website)
            const fullURL = `${baseURL}/${responseData.sURL}`;
            urlText.textContent = fullURL;

            textHolder.style.display = 'block';
            copyBtn.style.display = 'inline-block';

            copyBtn.addEventListener('click', function() {
                navigator.clipboard.writeText(urlText.textContent)
                    .then(() => alert("Text copied to clipboard"))
                    .catch(err => alert("Failed to copy text"));
            });
        } else {
            // If error response
            responseMessage.textContent = `Error: ${responseData.error} (Status Code: ${response.status})`;
            responseMessage.className = 'response-message error';
            textHolder.style.display = 'none';
            copyBtn.style.display = 'none';
        }
    } catch (error) {
        alert("There was an error processing the request.");
    }
}
