// On button click, sends empty POST request to API endpoint for forcing a run and shows a relevant alert when a response is received.
$(document).ready(function() {
    $('#force-button').bind('click', function(){
        // Disable the button and close existing alert
        $('#force-button').prop('disabled', true);
        $('#force-alert').alert('close')

        url =  window.location.href + 'api/v1/forceRun';
        $.ajax({
            type: 'POST',
            url: url,
            data: {},
            dataType: "json",
            success:function(data) {
                showForceAlert(true, data.message)
                $('#force-button').prop('disabled', false);
            },
            error:function() {
                showForceAlert(false, 'Server error attempting to force a run. See container logs for more info.')
                $('#force-button').prop('disabled', false);
            }
        });
    });
});

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
