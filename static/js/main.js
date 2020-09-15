// On button click, sends empty POST request to API endpoint for forcing a run and shows a relevant alert when a response is received.
$(document).ready(function() {
    $('#force-full-button').bind('click', function(){
        // Disable the buttons and close existing alert
        $('.force-button').each(function(){ $(this).prop('disabled', true); });
        $('#force-alert').alert('close')

        forceRun(false)
    });

    $('#force-failed-button').bind('click', function(){
        // Disable the buttons and close existing alert
        $('.force-button').each(function(){ $(this).prop('disabled', true); });
        $('#force-alert').alert('close')

        forceRun(true)
    });
});

// Send an XHR request to the server to force a run.
function forceRun(failed) {
    url =  window.location.href + 'api/v1/forceRun';
    $.ajax({
        type: 'POST',
        url: url,
        data: {failed: failed},
        dataType: "json",
        success:function(data) {
            showForceAlert(true, data.message)
            $('.force-button').each(function(){ $(this).prop('disabled', false); });
        },
        error:function() {
            showForceAlert(false, 'Server error attempting to force a run. See container logs for more info.')
            $('.force-button').each(function(){ $(this).prop('disabled', false); });
        }
    });
}

// Show a relevant alert message, styled based on the "success" of the associated response.
function showForceAlert(success, message) {
    alertClass = success ? 'success' : 'warning';
    $('#force-alert-container').empty();
    $('#force-alert-container').append(
        '<div id="force-alert" class="alert alert-' + alertClass + ' alert-dismissible show" role="alert">' +
            '<button type="button" class="close" data-dismiss="alert" aria-label="Close">' +
                '<span aria-hidden="true">×</span>' +
            '</button>' + message +
        '</div>');
}
