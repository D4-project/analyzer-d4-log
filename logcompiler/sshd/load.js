function imgLoad(image) {
    'use strict';
    // Create new promise with the Promise() constructor;
    // This has as its argument a function with two parameters, resolve and reject
    return new Promise(function (resolve, reject) {
        // Standard XHR to load an image
        var request = new XMLHttpRequest();
        var url = 'http://127.0.0.1:4444/data/sshd/'+image+'.svg'
        request.open('GET', url);
        request.responseType = 'blob';
        
        // When the request loads, check whether it was successful
        request.onload = function () {
            if (request.status === 200) {
                // If successful, resolve the promise by passing back the request response
                resolve(request.response);
            } else {
                // If it fails, reject the promise with a error message
                reject(new Error('Image didn\'t load successfully; error code:' + request.statusText));
            }
        };
      
        request.onerror = function () {
            // Also deal with the case when the entire request fails to begin with
            // This is probably a network error, so reject the promise with an appropriate message
            reject(new Error('There was a network error.'));
        };
      
        // Send the request
        request.send();
    });
}

function loadImage(date, type) {
    'use strict';
    console.log(date);
    console.log(type);
    // Get a reference to the body element, and create a new image object
    var holder = document.querySelector('#imageholder'),
    myImage = new Image();
    myImage.crossOrigin = ""; // or "anonymous"
    
    // Call the function with the URL we want to load, but then chain the
    // promise then() method on to the end of it. This contains two callbacks
    imgLoad(date+'/'+date+':'+type).then(function (response) {
        // The first runs when the promise resolves, with the request.reponse specified within the resolve() method.
        var imageURL = window.URL.createObjectURL(response);
        myImage.src = imageURL;
        holder.innerHTML = "";
        holder.appendChild(myImage);
        // The second runs when the promise is rejected, and logs the Error specified with the reject() method.
    }, function (Error) {
        console.log(Error);
    });
}