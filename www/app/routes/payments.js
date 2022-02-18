import Ember from 'ember';
import Payment from "../models/payment";
import config from '../config/environment';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),

  model: function() {
    var url = config.APP.ApiUrl + 'api/payments';
    return Ember.$.ajax({
      method: 'get',
      url: url,
      crossDomain: true,
      xhrFields: {
        withCredentials: true
      },
    }).then(function(data) {
			if (data.payments) {
				data.payments = data.payments.map(function(p) {
					return Payment.create(p);
				});
			}
			return data;
    });
	},

  beforeModel() {
    if(!this.get('auth').isLoggedIn()) {
        this.transitionTo('login');
    }
  },

  setupController: function(controller, model) {
    this._super(controller, model);
    Ember.run.later(this, this.refresh, 5000);
  }
});
